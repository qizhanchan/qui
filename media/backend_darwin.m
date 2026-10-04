//go:build darwin && cgo
// ^ Advisory for IDE; file is compiled only on darwin because of .m
// + _darwin suffix.
//
// Darwin media backend — Phase 1 (audio-only).
//
// Wraps AVAudioPlayer for file-based audio playback (mp3/m4a/aac/wav).
// AVAudioPlayer gives us play / pause / stop / seek / volume / rate /
// position out of the box, and an end-of-playback delegate callback.
// Phase 2 will add a second code path (AVAssetReader +
// VTDecompressionSession) for video files, and Phase 3 will swap
// audio to AVAudioEngine so an mp4's extracted PCM audio can feed
// the same renderer interface.
//
// Threading: AVAudioPlayer delegate callbacks fire on the thread the
// player was created on. The Go caller is expected to call Open from
// the main goroutine (which is OS-thread-locked in app.go), so the
// end-of-playback callback also lands on main — convenient for the
// common case where it triggers a redraw. Callers that open from a
// worker goroutine should hop to main themselves if their OnEnded
// touches widget state.

#import <Foundation/Foundation.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <CoreVideo/CVOpenGLTextureCache.h>
#import <OpenGL/OpenGL.h>
#import <OpenGL/gl3.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>

// Function signatures match the declarations in the cgo preamble of
// backend_darwin.go; cgo links by name. The project convention
// (ime_darwin.m, dialog_darwin.m) is to keep declarations only in
// the cgo preamble and let the .m file define them independently.

// Forward declarations of the Go-side //export functions from
// backend_darwin.go. cgo generates _cgo_export.h with these but
// declaring them inline matches the existing project convention
// (see ime_darwin.m) and keeps the .m self-contained.
extern void quiAudioDidEnd(uintptr_t handle);
extern void quiVideoDecodeOutput(uintptr_t handle, void* pixBuf, int64_t ptsNanos);
extern void quiVideoAudioDidEnd(uintptr_t handle);

// Forward declaration so the decode loop (defined earlier in the
// file than quiVideoBindTexture) can call the debug-flag helper.
static int mediaDebugEnabled(void);

// ----- ObjC player wrapper -----------------------------------------

@interface QuiAudioPlayer : NSObject <AVAudioPlayerDelegate>
@property (nonatomic, strong) AVAudioPlayer* player;
@property (nonatomic) uintptr_t handle;
@property (nonatomic) int codecID;     // 0 unknown, 1 aac, 2 mp3, 3 lpcm
@property (nonatomic) int sampleRate;  // 0 if not enumerable
@end

@implementation QuiAudioPlayer
- (void)audioPlayerDidFinishPlaying:(AVAudioPlayer*)player successfully:(BOOL)flag {
    // Fires when the buffer reaches its end. `flag` is NO if a decode
    // error halted playback mid-stream; we surface both as "ended"
    // since the user can call Position() to detect partial playback.
    (void)flag;
    quiAudioDidEnd(self.handle);
}
@end

// ----- Handle registry (ObjC side) ---------------------------------
//
// Mirrors the Go-side registry in backend_darwin.go: integer handles
// thread the cgo boundary, the actual ObjC object lives here. Both
// sides agree on the handle value chosen at open time.

static NSMutableDictionary<NSNumber*, QuiAudioPlayer*>* gPlayers;
static NSLock* gPlayersLock;
static _Atomic uintptr_t gNextHandle = 1;

static void ensureRegistry(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        gPlayers = [NSMutableDictionary new];
        gPlayersLock = [[NSLock alloc] init];
    });
}

static QuiAudioPlayer* lookupPlayer(uintptr_t handle) {
    ensureRegistry();
    [gPlayersLock lock];
    QuiAudioPlayer* p = gPlayers[@(handle)];
    [gPlayersLock unlock];
    return p;
}

// ----- Codec / track metadata via AVURLAsset ------------------------
//
// AVAudioPlayer doesn't expose codec / sample-rate on every macOS
// version, but AVURLAsset always does (it's the same engine that
// powers AVAssetReader in Phase 2). One asset open per file at
// load time; cheap.

static void fillTrackInfo(NSURL* url, int* outSampleRate, int* outCodecID) {
    if (outSampleRate) *outSampleRate = 0;
    if (outCodecID) *outCodecID = 0;
    if (!url) return;

    @autoreleasepool {
        AVURLAsset* asset = [AVURLAsset URLAssetWithURL:url options:nil];
        // tracksWithMediaType: is deprecated in macOS 15 in favor of
        // the async loadTracksWithMediaType:completionHandler:, but
        // the sync API still works everywhere. We accept the warning
        // locally rather than gate behind @available. Phase 2's
        // AVAssetReader code will move to the async API.
        #pragma clang diagnostic push
        #pragma clang diagnostic ignored "-Wdeprecated-declarations"
        NSArray<AVAssetTrack*>* tracks = [asset tracksWithMediaType:AVMediaTypeAudio];
        #pragma clang diagnostic pop
        if (tracks.count == 0) return;
        AVAssetTrack* track = tracks.firstObject;
        NSArray* descs = track.formatDescriptions;
        if (descs.count == 0) return;
        CMAudioFormatDescriptionRef desc = (__bridge CMAudioFormatDescriptionRef)descs.firstObject;
        const AudioStreamBasicDescription* asbd =
            CMAudioFormatDescriptionGetStreamBasicDescription(desc);
        if (!asbd) return;

        if (outSampleRate) *outSampleRate = (int)asbd->mSampleRate;
        if (outCodecID) {
            switch (asbd->mFormatID) {
                case kAudioFormatMPEG4AAC:
                case kAudioFormatMPEG4AAC_HE:
                case kAudioFormatMPEG4AAC_LD:
                    *outCodecID = 1;
                    break;
                case kAudioFormatMPEGLayer3:
                    *outCodecID = 2;
                    break;
                case kAudioFormatLinearPCM:
                    *outCodecID = 3;
                    break;
                default:
                    *outCodecID = 0;
                    break;
            }
        }
    }
}

// ----- C interface -------------------------------------------------

uintptr_t quiAudioOpen(const char* path, char** errOut) {
    if (errOut) *errOut = NULL;
    if (!path) {
        if (errOut) *errOut = strdup("nil path");
        return 0;
    }
    @autoreleasepool {
        ensureRegistry();

        NSString* nsPath = [NSString stringWithUTF8String:path];
        if (!nsPath) {
            if (errOut) *errOut = strdup("invalid utf-8 path");
            return 0;
        }
        NSURL* url = [NSURL fileURLWithPath:nsPath];

        NSError* err = nil;
        AVAudioPlayer* avp = [[AVAudioPlayer alloc] initWithContentsOfURL:url
                                                                    error:&err];
        if (!avp || err) {
            NSString* msg = err.localizedDescription ?: @"AVAudioPlayer init failed";
            if (errOut) *errOut = strdup([msg UTF8String]);
            return 0;
        }
        if (![avp prepareToPlay]) {
            if (errOut) *errOut = strdup("prepareToPlay failed");
            return 0;
        }
        avp.enableRate = YES;  // required for AVAudioPlayer.rate to take effect

        QuiAudioPlayer* qp = [[QuiAudioPlayer alloc] init];
        qp.player = avp;
        avp.delegate = qp;
        int sr = 0, cid = 0;
        fillTrackInfo(url, &sr, &cid);
        qp.sampleRate = sr;
        qp.codecID = cid;

        uintptr_t h = atomic_fetch_add(&gNextHandle, 1);
        qp.handle = h;

        [gPlayersLock lock];
        gPlayers[@(h)] = qp;
        [gPlayersLock unlock];

        return h;
    }
}

double quiAudioDuration(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return 0;
    return qp.player.duration;
}

int quiAudioSampleRate(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return 0;
    return qp.sampleRate;
}

int quiAudioChannels(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return 0;
    return (int)qp.player.numberOfChannels;
}

int quiAudioCodecID(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return 0;
    return qp.codecID;
}

void quiAudioPlay(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return;
    [qp.player play];
}

void quiAudioPause(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return;
    [qp.player pause];
}

void quiAudioStop(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return;
    [qp.player stop];
    qp.player.currentTime = 0;  // rewind so next Play starts from 0
}

void quiAudioSeekTo(uintptr_t handle, double seconds) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return;
    if (seconds < 0) seconds = 0;
    NSTimeInterval dur = qp.player.duration;
    if (seconds > dur) seconds = dur;
    qp.player.currentTime = seconds;
}

double quiAudioPosition(uintptr_t handle) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return 0;
    return qp.player.currentTime;
}

void quiAudioSetVolume(uintptr_t handle, float v) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return;
    if (v < 0) v = 0;
    if (v > 1) v = 1;
    qp.player.volume = v;
}

void quiAudioSetRate(uintptr_t handle, float r) {
    QuiAudioPlayer* qp = lookupPlayer(handle);
    if (!qp) return;
    // AVAudioPlayer.rate range is [0.5, 2.0]; clamp so the assignment
    // doesn't silently get rejected.
    if (r < 0.5f) r = 0.5f;
    if (r > 2.0f) r = 2.0f;
    qp.player.rate = r;
}

void quiAudioClose(uintptr_t handle) {
    @autoreleasepool {
        ensureRegistry();
        [gPlayersLock lock];
        QuiAudioPlayer* qp = gPlayers[@(handle)];
        [gPlayers removeObjectForKey:@(handle)];
        [gPlayersLock unlock];

        if (qp) {
            [qp.player stop];
            qp.player.delegate = nil;
            qp.player = nil;
            // qp itself dealloced by ARC when the dictionary's strong
            // ref drops above and this local scope ends.
        }
    }
}

// ===================================================================
//  Video pipeline (Phase 2)
// ===================================================================
//
// AVAssetReader-backed playback decoder + isolated scrub decoder.
// The two share an AVURLAsset but otherwise hold independent
// readers, dispatch queues, and CVOpenGLTextureCaches so frame-by-
// frame scrub (DecodeFrameAt) never disturbs streaming playback.
//
// Decode loop lives on a per-source serial dispatch queue. Each
// iteration claims a slot via dispatch_semaphore_wait (sized to
// match the Go-side channel cap so the channel never overflows),
// reads one sample buffer, CFRetains the embedded CVPixelBuffer,
// and pushes it across cgo via quiVideoDecodeOutput. The Go
// consumer signals quiVideoFrameConsumed after each pop to release
// the slot back to the semaphore.
//
// Threading: the GL texture cache and its bind / flush calls run
// on the main thread (where the GL context is current). The decode
// loop never touches GL — it only produces CVPixelBuffers.

// kVideoQueueCap MUST match videoQueueCap in backend_darwin.go.
// Both sides agree on the slot count; mismatching them would either
// stall the decoder (cap too low) or risk channel overflow (too high).
#define kVideoQueueCap 3

@interface QuiVideoSource : NSObject {
    // Atomic ivars exposed for cross-thread access in runDecodeLoop /
    // quiVideoSeek / quiVideoClose. ObjC properties would box these
    // through getter/setter and lose atomic_* semantics.
@public
    atomic_int  shouldExit;
    atomic_int  seekPending;
    atomic_int  seekPrecise;
    atomic_llong seekTargetNs;
}
@property (nonatomic) uintptr_t handle;
@property (nonatomic, strong) AVURLAsset* asset;
@property (nonatomic, strong) AVAssetTrack* videoTrack;
@property (nonatomic, strong) AVAssetReader* reader;       // nil between seeks
@property (nonatomic, strong) AVAssetReaderTrackOutput* output;
@property (nonatomic, strong) dispatch_queue_t decodeQueue;
@property (nonatomic, strong) dispatch_semaphore_t slotSem; // empty slots in Go channel
@property (nonatomic, strong) dispatch_semaphore_t seekDone;
@property (nonatomic) int width;
@property (nonatomic) int height;
@property (nonatomic) double fps;
@property (nonatomic) double duration;       // seconds
@property (nonatomic) int codecID;           // 1 h264, 2 hevc
@end

@implementation QuiVideoSource
@end

@interface QuiVideoScrubDecoder : NSObject
@property (nonatomic) uintptr_t handle;
@property (nonatomic, strong) AVURLAsset* asset;
@property (nonatomic, strong) AVAssetTrack* videoTrack;
@property (nonatomic, strong) AVAssetReader* reader;
@property (nonatomic, strong) AVAssetReaderTrackOutput* output;
@end

@implementation QuiVideoScrubDecoder
@end

// Single global CVOpenGLTextureCache. CVOpenGLTextureCache is tied
// to a GL context, not to a source — and qui has exactly one GL
// context per app. Sharing the cache lets us bind frames from
// playback sources and scrub decoders without each owning state.
// Lazily created in quiVideoBindTexture on the main thread.
static CVOpenGLTextureCacheRef gTextureCache = NULL;

// ----- Video registry (ObjC side) ---------------------------------------

static NSMutableDictionary<NSNumber*, QuiVideoSource*>* gVideoSources;
static NSMutableDictionary<NSNumber*, QuiVideoScrubDecoder*>* gScrubDecoders;
static NSLock* gVideoLock;
static _Atomic uintptr_t gNextVideoHandle = 1;

static void ensureVideoRegistry(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        gVideoSources = [NSMutableDictionary new];
        gScrubDecoders = [NSMutableDictionary new];
        gVideoLock = [[NSLock alloc] init];
    });
}

static QuiVideoSource* lookupVideoSourceOC(uintptr_t handle) {
    ensureVideoRegistry();
    [gVideoLock lock];
    QuiVideoSource* s = gVideoSources[@(handle)];
    [gVideoLock unlock];
    return s;
}

static QuiVideoScrubDecoder* lookupScrubDecoderOC(uintptr_t handle) {
    ensureVideoRegistry();
    [gVideoLock lock];
    QuiVideoScrubDecoder* d = gScrubDecoders[@(handle)];
    [gVideoLock unlock];
    return d;
}

// ----- Helpers ----------------------------------------------------------

// codecIDFromFormat maps a CMFormatDescription codec type to our 1/2/0 enum.
static int codecIDFromFormat(CMFormatDescriptionRef desc) {
    if (!desc) return 0;
    CMVideoCodecType c = CMFormatDescriptionGetMediaSubType(desc);
    switch (c) {
        case kCMVideoCodecType_H264: return 1;
        case kCMVideoCodecType_HEVC: return 2;
        default: return 0;
    }
}

// outputSettings configures AVAssetReaderTrackOutput to deliver BGRA
// pixel buffers compatible with both OpenGL texture cache and Metal
// (in case we migrate). Setting kCVPixelBufferOpenGLCompatibilityKey
// forces GL_TEXTURE_2D over GL_TEXTURE_RECTANGLE on modern macOS.
static NSDictionary* videoOutputSettings(void) {
    return @{
        (id)kCVPixelBufferPixelFormatTypeKey:    @(kCVPixelFormatType_32BGRA),
        (id)kCVPixelBufferOpenGLCompatibilityKey: @YES,
        (id)kCVPixelBufferIOSurfacePropertiesKey: @{},
    };
}

// buildReaderFromTime constructs a fresh AVAssetReader + output for
// the given time range. AVAssetReader is single-shot; every seek
// tears it down and builds a new one. The reader's underlying
// VTDecompressionSession is driven implicitly by the output's
// pixel-buffer settings.
static BOOL buildReader(AVURLAsset* asset, AVAssetTrack* track,
                        CMTime start, NSError** outErr,
                        AVAssetReader** outReader,
                        AVAssetReaderTrackOutput** outOutput) {
    AVAssetReader* r = [[AVAssetReader alloc] initWithAsset:asset error:outErr];
    if (!r) return NO;
    if (CMTIME_IS_VALID(start) && CMTimeCompare(start, kCMTimeZero) > 0) {
        r.timeRange = CMTimeRangeMake(start, kCMTimePositiveInfinity);
    }
    AVAssetReaderTrackOutput* o =
        [AVAssetReaderTrackOutput assetReaderTrackOutputWithTrack:track
                                                    outputSettings:videoOutputSettings()];
    o.alwaysCopiesSampleData = NO;
    if (![r canAddOutput:o]) {
        if (outErr) *outErr = [NSError errorWithDomain:@"qui.media" code:1
                                              userInfo:@{NSLocalizedDescriptionKey: @"cannot add reader output"}];
        return NO;
    }
    [r addOutput:o];
    if (![r startReading]) {
        if (outErr) *outErr = r.error ?: [NSError errorWithDomain:@"qui.media" code:2
                                                          userInfo:@{NSLocalizedDescriptionKey: @"reader startReading failed"}];
        return NO;
    }
    *outReader = r;
    *outOutput = o;
    return YES;
}

// ----- Decode loop ------------------------------------------------------

// runDecodeLoop drains the current reader, pushing each decoded frame
// to Go. Exits when EOF / cancellation / seek-requested. After EOF
// it parks the loop until shouldExit or seekPending wakes it.
static void runDecodeLoop(QuiVideoSource* s) {
    // Decoder-local mirror of the seek target, used by the precise-PTS
    // drop filter below. Must NOT be the shared atomic seekTargetNs —
    // that one is owned by quiVideoSeek (writer). If the decoder also
    // cleared seekTargetNs to 0 after a survivor frame, it could race
    // a concurrent quiVideoSeek's set: the writer stores the new
    // target, the decoder clears it to 0, the writer stores seekPending,
    // and the next iter reads target=0 from the atomic and rebuilds at
    // time 0 — picture jumps to the start of the video, very visible
    // during a slider drag because every OnChange fires another seek.
    int64_t filterTargetNs = 0;
    while (!atomic_load(&s->shouldExit)) {
        // Honor a pending seek before claiming a slot — flushes the
        // old reader and builds a fresh one at the target time.
        if (atomic_exchange(&s->seekPending, 0)) {
            int64_t targetNs = atomic_load(&s->seekTargetNs);
            int precise = atomic_load(&s->seekPrecise);
            CMTime target = CMTimeMake(targetNs, 1000000000);
            // Tear down the old reader and rebuild from target time.
            // AVAssetReader cannot seek in place — every seek pays the
            // build cost (~1ms on M-series for a 720p source).
            [s.reader cancelReading];
            s.reader = nil;
            s.output = nil;
            NSError* err = nil;
            AVAssetReader* r = nil; AVAssetReaderTrackOutput* o = nil;
            if (!buildReader(s.asset, s.videoTrack, target, &err, &r, &o)) {
                // Seek failed; surface as EOF for now (no error path
                // wired through the //export callback in v1).
                dispatch_semaphore_signal(s.seekDone);
                return;
            }
            s.reader = r;
            s.output = o;
            (void)precise;
            // Stash the target locally so the drop filter checks it
            // without re-reading the shared atomic on every frame.
            filterTargetNs = targetNs;
            dispatch_semaphore_signal(s.seekDone);
        }

        // Wait for a free slot in the Go-side channel. Time out so
        // we periodically re-check the exit / seek flags.
        if (dispatch_semaphore_wait(s.slotSem,
                                    dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC)) != 0) {
            continue;
        }

        CMSampleBufferRef sb = NULL;
        @autoreleasepool {
            sb = [s.output copyNextSampleBuffer];
        }
        if (sb == NULL) {
            // EOF or reader failed. Return the slot we claimed and
            // park until a seek arrives or we're told to exit.
            dispatch_semaphore_signal(s.slotSem);
            while (!atomic_load(&s->shouldExit) && !atomic_load(&s->seekPending)) {
                [NSThread sleepForTimeInterval:0.05];
            }
            continue;
        }

        CMTime pts = CMSampleBufferGetPresentationTimeStamp(sb);
        int64_t ptsNs = (int64_t)(CMTimeGetSeconds(pts) * 1e9);

        // Precise-seek drop: skip frames whose PTS lies before the
        // requested target. filterTargetNs is decoder-local; clearing
        // it after the first survivor avoids per-frame compares without
        // touching the shared atomic (which a concurrent quiVideoSeek
        // may be writing).
        if (filterTargetNs > 0 && ptsNs < filterTargetNs) {
            CFRelease(sb);
            // Slot was consumed by this skipped sample — replace it.
            dispatch_semaphore_signal(s.slotSem);
            continue;
        }
        filterTargetNs = 0;

        CVPixelBufferRef pb = CMSampleBufferGetImageBuffer(sb);
        if (pb == NULL) {
            CFRelease(sb);
            dispatch_semaphore_signal(s.slotSem);
            continue;
        }
        CFRetain(pb);
        CFRelease(sb);

        if (mediaDebugEnabled()) {
            static int decoded = 0;
            decoded++;
            if (decoded % 30 == 0) {
                size_t w = CVPixelBufferGetWidth(pb);
                size_t h = CVPixelBufferGetHeight(pb);
                OSType pf = CVPixelBufferGetPixelFormatType(pb);
                NSLog(@"[media] decoded #%d ptsNs=%lld %zux%zu pf=%c%c%c%c",
                      decoded, ptsNs, w, h,
                      (char)((pf >> 24) & 0xff),
                      (char)((pf >> 16) & 0xff),
                      (char)((pf >> 8) & 0xff),
                      (char)(pf & 0xff));
            }
        }

        quiVideoDecodeOutput(s.handle, (void*)pb, ptsNs);
    }
}

// ----- C interface: open / info -----------------------------------------

uintptr_t quiVideoOpen(const char* path, char** errOut) {
    if (errOut) *errOut = NULL;
    if (!path) {
        if (errOut) *errOut = strdup("nil path");
        return 0;
    }
    @autoreleasepool {
        ensureVideoRegistry();
        NSString* nsPath = [NSString stringWithUTF8String:path];
        if (!nsPath) {
            if (errOut) *errOut = strdup("invalid utf-8 path");
            return 0;
        }
        NSURL* url = [NSURL fileURLWithPath:nsPath];
        AVURLAsset* asset = [AVURLAsset URLAssetWithURL:url options:nil];

        #pragma clang diagnostic push
        #pragma clang diagnostic ignored "-Wdeprecated-declarations"
        NSArray<AVAssetTrack*>* tracks = [asset tracksWithMediaType:AVMediaTypeVideo];
        #pragma clang diagnostic pop
        if (tracks.count == 0) {
            if (errOut) *errOut = strdup("no video track");
            return 0;
        }
        AVAssetTrack* track = tracks.firstObject;

        NSError* err = nil;
        AVAssetReader* reader = nil; AVAssetReaderTrackOutput* output = nil;
        if (!buildReader(asset, track, kCMTimeZero, &err, &reader, &output)) {
            NSString* msg = err.localizedDescription ?: @"reader init failed";
            if (errOut) *errOut = strdup([msg UTF8String]);
            return 0;
        }

        QuiVideoSource* s = [QuiVideoSource new];
        uintptr_t h = atomic_fetch_add(&gNextVideoHandle, 1);
        s.handle = h;
        s.asset = asset;
        s.videoTrack = track;
        s.reader = reader;
        s.output = output;
        s.decodeQueue = dispatch_queue_create("qui.media.decode", DISPATCH_QUEUE_SERIAL);
        s.slotSem = dispatch_semaphore_create(kVideoQueueCap);
        s.seekDone = dispatch_semaphore_create(0);

        CGSize natural = track.naturalSize;
        // Apply preferredTransform so rotated content (portrait
        // recordings) reports the on-screen dimensions, not the raw
        // sensor orientation.
        CGSize transformed = CGSizeApplyAffineTransform(natural, track.preferredTransform);
        s.width = (int)fabs(transformed.width);
        s.height = (int)fabs(transformed.height);
        s.fps = (double)track.nominalFrameRate;
        s.duration = CMTimeGetSeconds(asset.duration);

        NSArray* descs = track.formatDescriptions;
        if (descs.count > 0) {
            CMFormatDescriptionRef d = (__bridge CMFormatDescriptionRef)descs.firstObject;
            s.codecID = codecIDFromFormat(d);
        }

        [gVideoLock lock];
        gVideoSources[@(h)] = s;
        [gVideoLock unlock];
        return h;
    }
}

int quiVideoWidth(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    return s ? s.width : 0;
}
int quiVideoHeight(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    return s ? s.height : 0;
}
double quiVideoFPS(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    return s ? s.fps : 0;
}
double quiVideoDuration(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    return s ? s.duration : 0;
}
int quiVideoCodecID(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    return s ? s.codecID : 0;
}

void quiVideoStart(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    if (!s) return;
    dispatch_async(s.decodeQueue, ^{
        runDecodeLoop(s);
    });
}

void quiVideoSeek(uintptr_t handle, double seconds, int precise) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    if (!s) return;
    int64_t targetNs = (int64_t)(seconds * 1e9);
    if (!precise) targetNs = 0;  // disable PTS-drop filter for snap-to-IDR
    atomic_store(&s->seekPrecise, precise);
    atomic_store(&s->seekTargetNs, targetNs);
    atomic_store(&s->seekPending, 1);
    // Wake the decode loop if it's waiting on a slot.
    dispatch_semaphore_signal(s.slotSem);
    // Block until the decode loop acknowledges the rebuild so callers
    // can immediately Drain stale frames and expect post-seek frames
    // next.
    dispatch_semaphore_wait(s.seekDone, dispatch_time(DISPATCH_TIME_NOW, 500 * NSEC_PER_MSEC));
}

void quiVideoFrameConsumed(uintptr_t handle) {
    QuiVideoSource* s = lookupVideoSourceOC(handle);
    if (!s) return;
    dispatch_semaphore_signal(s.slotSem);
}

void quiVideoClose(uintptr_t handle) {
    @autoreleasepool {
        ensureVideoRegistry();
        [gVideoLock lock];
        QuiVideoSource* s = gVideoSources[@(handle)];
        [gVideoSources removeObjectForKey:@(handle)];
        [gVideoLock unlock];
        if (!s) return;

        atomic_store(&s->shouldExit, 1);
        // Bump the semaphore enough to unblock any pending wait.
        for (int i = 0; i < kVideoQueueCap + 1; i++) {
            dispatch_semaphore_signal(s.slotSem);
        }
        [s.reader cancelReading];
        s.reader = nil;
        s.output = nil;
        // ARC drops the QuiVideoSource when this scope ends. The
        // dispatch queue retains itself until the decode block returns,
        // which it will once it sees shouldExit.
    }
}

// ----- C interface: scrub ----------------------------------------------

uintptr_t quiVideoOpenScrub(uintptr_t srcHandle, char** errOut) {
    if (errOut) *errOut = NULL;
    QuiVideoSource* src = lookupVideoSourceOC(srcHandle);
    if (!src) {
        if (errOut) *errOut = strdup("source handle not found");
        return 0;
    }
    @autoreleasepool {
        QuiVideoScrubDecoder* d = [QuiVideoScrubDecoder new];
        uintptr_t h = atomic_fetch_add(&gNextVideoHandle, 1);
        d.handle = h;
        d.asset = src.asset;
        d.videoTrack = src.videoTrack;
        // Reader is lazy — built on each DecodeAt call.
        [gVideoLock lock];
        gScrubDecoders[@(h)] = d;
        [gVideoLock unlock];
        return h;
    }
}

int quiVideoScrubDecodeAt(uintptr_t scrubHandle, double seconds,
                          void** outPixBuf, int64_t* outPTSNs) {
    if (outPixBuf) *outPixBuf = NULL;
    if (outPTSNs) *outPTSNs = 0;
    QuiVideoScrubDecoder* d = lookupScrubDecoderOC(scrubHandle);
    if (!d) return 0;

    @autoreleasepool {
        // Reach back 0.5s to ensure we cover the nearest IDR before
        // the requested time. AVAssetReader snaps the time range to
        // the IDR ≤ start, so this guarantees we get a decodable
        // sample sequence ending at-or-after `seconds`.
        double startSec = seconds - 0.5;
        if (startSec < 0) startSec = 0;
        CMTime start = CMTimeMakeWithSeconds(startSec, 600);

        // Tear down any prior reader — AVAssetReader is single-shot.
        [d.reader cancelReading];
        d.reader = nil;
        d.output = nil;

        NSError* err = nil;
        AVAssetReader* r = nil; AVAssetReaderTrackOutput* o = nil;
        if (!buildReader(d.asset, d.videoTrack, start, &err, &r, &o)) {
            return 0;
        }
        d.reader = r;
        d.output = o;

        int64_t targetNs = (int64_t)(seconds * 1e9);
        while (1) {
            CMSampleBufferRef sb = [d.output copyNextSampleBuffer];
            if (!sb) return 0;
            CMTime pts = CMSampleBufferGetPresentationTimeStamp(sb);
            int64_t ptsNs = (int64_t)(CMTimeGetSeconds(pts) * 1e9);
            if (ptsNs < targetNs) {
                CFRelease(sb);
                continue;
            }
            CVPixelBufferRef pb = CMSampleBufferGetImageBuffer(sb);
            if (!pb) {
                CFRelease(sb);
                return 0;
            }
            CFRetain(pb);
            CFRelease(sb);
            if (outPixBuf) *outPixBuf = (void*)pb;
            if (outPTSNs) *outPTSNs = ptsNs;
            return 1;
        }
    }
}

void quiVideoCloseScrub(uintptr_t scrubHandle) {
    @autoreleasepool {
        ensureVideoRegistry();
        [gVideoLock lock];
        QuiVideoScrubDecoder* d = gScrubDecoders[@(scrubHandle)];
        [gScrubDecoders removeObjectForKey:@(scrubHandle)];
        [gVideoLock unlock];
        if (!d) return;
        [d.reader cancelReading];
        d.reader = nil;
        d.output = nil;
    }
}

// ----- C interface: GL texture binding ---------------------------------
//
// MUST be called from the main thread with the GL context current
// (i.e. from inside a GPUCanvas.QueueGLDraw callback). The cache is
// created lazily on first call so we don't need a separate "GL init"
// hook.

// mediaDebugEnabled mirrors the Go-side mediaDebug. Queried lazily
// on first use; result cached for subsequent calls.
static int gMediaDebug = -1;
static int mediaDebugEnabled(void) {
    if (gMediaDebug == -1) {
        const char* env = getenv("QUI_MEDIA_DEBUG");
        gMediaDebug = (env && env[0] != '\0') ? 1 : 0;
    }
    return gMediaDebug;
}

int quiVideoBindTexture(void* pixBuf, void** outCVTex, uint32_t* outTexName) {
    if (outCVTex) *outCVTex = NULL;
    if (outTexName) *outTexName = 0;
    if (!pixBuf) {
        if (mediaDebugEnabled()) NSLog(@"[media] quiVideoBindTexture: nil pixBuf");
        return 0;
    }

    if (gTextureCache == NULL) {
        CGLContextObj cgl = CGLGetCurrentContext();
        if (!cgl) {
            if (mediaDebugEnabled()) NSLog(@"[media] quiVideoBindTexture: CGLGetCurrentContext returned NULL — not on main GL thread?");
            return 0;
        }
        CGLPixelFormatObj pf = CGLGetPixelFormat(cgl);
        CVReturn cr = CVOpenGLTextureCacheCreate(kCFAllocatorDefault, NULL,
                                                  cgl, pf, NULL, &gTextureCache);
        if (cr != kCVReturnSuccess || gTextureCache == NULL) {
            if (mediaDebugEnabled()) NSLog(@"[media] CVOpenGLTextureCacheCreate failed cr=%d", cr);
            return 0;
        }
        if (mediaDebugEnabled()) NSLog(@"[media] CVOpenGLTextureCache created on cgl=%p", cgl);
    }

    CVPixelBufferRef pb = (CVPixelBufferRef)pixBuf;
    CVOpenGLTextureRef tex = NULL;
    CVReturn cr = CVOpenGLTextureCacheCreateTextureFromImage(
        kCFAllocatorDefault, gTextureCache, pb, NULL, &tex);
    if (cr != kCVReturnSuccess || tex == NULL) {
        if (mediaDebugEnabled()) {
            OSType pf = CVPixelBufferGetPixelFormatType(pb);
            NSLog(@"[media] CVOpenGLTextureCacheCreateTextureFromImage failed cr=%d pixFmt=%c%c%c%c",
                  cr,
                  (char)((pf >> 24) & 0xff),
                  (char)((pf >> 16) & 0xff),
                  (char)((pf >> 8) & 0xff),
                  (char)(pf & 0xff));
        }
        return 0;
    }

    GLuint name = CVOpenGLTextureGetName(tex);
    GLenum target = CVOpenGLTextureGetTarget(tex);
    // GL_TEXTURE_RECTANGLE is what CVOpenGLTextureCache hands back
    // on macOS — IOSurface-backed textures use non-normalized texel
    // coords. We accept it; the Go side has a dedicated sampler2DRect
    // draw path in gl_rect_darwin.go. Older code paths that expect
    // GL_TEXTURE_2D would need a separate shader.
    if (target != GL_TEXTURE_2D && target != GL_TEXTURE_RECTANGLE) {
        if (mediaDebugEnabled()) NSLog(@"[media] unsupported texture target 0x%x", target);
        CFRelease(tex);
        return 0;
    }

    if (outCVTex) *outCVTex = (void*)tex;   // ownership transferred to caller
    if (outTexName) *outTexName = (uint32_t)name;
    return 1;
}

void quiVideoCacheFlush(void) {
    if (gTextureCache == NULL) return;
    CVOpenGLTextureCacheFlush(gTextureCache, 0);
}

void quiVideoReleaseGLTex(void* cvTex) {
    if (cvTex) CFRelease((CVOpenGLTextureRef)cvTex);
}

void quiVideoReleasePixBuf(void* pixBuf) {
    if (pixBuf) CFRelease((CVPixelBufferRef)pixBuf);
}

// ===================================================================
//  Video-source audio (Phase 3)
// ===================================================================
//
// AVAudioFile + AVAudioEngine + AVAudioPlayerNode driver for the
// audio track inside an mp4. Separate from QuiAudioPlayer because
// VideoView needs this renderer to expose a HostTime that drives
// A/V sync — not just to play the audio independently.
//
// Lifetime: created lazily inside openDarwinVideoSource if the file
// has a decodable audio track. AVAudioFile silently fails when the
// file has no audio; the Go side falls back to wallClock pacing in
// that case.
//
// Seeking model: we use scheduleSegment to play [startFrame ..
// end). Seek = stop player → schedule a new segment from the new
// frame → play. segmentOffset records the wall-time of the segment
// start so hostTime can add the player's local sample position to
// it without re-deriving from AVAudioFile state.

@interface QuiVideoAudio : NSObject {
@public
    atomic_int closed;
}
@property (nonatomic) uintptr_t handle;
@property (nonatomic, strong) AVAudioFile* audioFile;
@property (nonatomic, strong) AVAudioEngine* engine;
@property (nonatomic, strong) AVAudioPlayerNode* playerNode;
// rateNode is a time-pitch unit (NOT Varispeed). Varispeed changes
// both rate and pitch like a tape speed-up — voices sound chipmunky
// at 2x, muddy at 0.5x. TimePitch decouples them so leaving `pitch`
// at 0 preserves the source pitch while `rate` scales playback speed,
// matching how mainstream video players behave at 1.5x / 2x.
@property (nonatomic, strong) AVAudioUnitTimePitch* rateNode;
// segmentOffset is the audio-file time (seconds) at which the
// currently scheduled segment begins. Added to the player's local
// sample position to compute true media HostTime.
@property (nonatomic) NSTimeInterval segmentOffset;
// pausedHostTime is the latched HostTime at the moment we last
// paused; returned from hostTime while paused so the value doesn't
// regress to whatever lastRenderTime reports.
@property (nonatomic) NSTimeInterval pausedHostTime;
@property (nonatomic) BOOL paused;
@property (nonatomic) BOOL hasPlayed;
@end

@implementation QuiVideoAudio
@end

// Forward declaration so quiVideoAudioPause can call hostTime before
// its definition appears below.
double quiVideoAudioHostTime(uintptr_t handle);

static NSMutableDictionary<NSNumber*, QuiVideoAudio*>* gVideoAudios;
static _Atomic uintptr_t gNextVideoAudioHandle = 1;

static void ensureVideoAudioRegistry(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        gVideoAudios = [NSMutableDictionary new];
        // Reuse gVideoLock — both registries share the same mutex
        // because they're managed in tandem.
        ensureVideoRegistry();
    });
}

static QuiVideoAudio* lookupVideoAudio(uintptr_t handle) {
    ensureVideoAudioRegistry();
    [gVideoLock lock];
    QuiVideoAudio* a = gVideoAudios[@(handle)];
    [gVideoLock unlock];
    return a;
}

// scheduleFromOffset reschedules the player from the given file
// offset (seconds). Must be called with the player stopped — the
// caller restarts via -play afterwards if appropriate.
static BOOL scheduleFromOffset(QuiVideoAudio* a, NSTimeInterval offset) {
    if (!a.audioFile) return NO;
    double sr = a.audioFile.processingFormat.sampleRate;
    AVAudioFramePosition startFrame = (AVAudioFramePosition)(offset * sr);
    if (startFrame < 0) startFrame = 0;
    if (startFrame >= a.audioFile.length) {
        a.segmentOffset = offset;
        return NO;
    }
    AVAudioFrameCount framesToPlay = (AVAudioFrameCount)(a.audioFile.length - startFrame);
    a.segmentOffset = offset;
    uintptr_t h = a.handle;
    [a.playerNode scheduleSegment:a.audioFile
                    startingFrame:startFrame
                       frameCount:framesToPlay
                           atTime:nil
                completionHandler:^{
        // Fires when the segment is fully consumed (EOF). Runs on
        // a CoreAudio thread; the Go callback hops nothing.
        quiVideoAudioDidEnd(h);
    }];
    return YES;
}

// ----- C interface --------------------------------------------------

uintptr_t quiVideoAudioOpen(const char* path, char** errOut) {
    if (errOut) *errOut = NULL;
    if (!path) {
        if (errOut) *errOut = strdup("nil path");
        return 0;
    }
    @autoreleasepool {
        ensureVideoAudioRegistry();
        NSString* nsPath = [NSString stringWithUTF8String:path];
        if (!nsPath) {
            if (errOut) *errOut = strdup("invalid utf-8 path");
            return 0;
        }
        NSURL* url = [NSURL fileURLWithPath:nsPath];

        NSError* err = nil;
        AVAudioFile* file = [[AVAudioFile alloc] initForReading:url error:&err];
        if (!file || err) {
            NSString* msg = err.localizedDescription ?: @"AVAudioFile init failed";
            if (errOut) *errOut = strdup([msg UTF8String]);
            return 0;
        }
        if (file.length <= 0) {
            if (errOut) *errOut = strdup("zero-length audio");
            return 0;
        }

        QuiVideoAudio* a = [QuiVideoAudio new];
        a.audioFile = file;
        a.engine = [[AVAudioEngine alloc] init];
        a.playerNode = [[AVAudioPlayerNode alloc] init];
        a.rateNode = [[AVAudioUnitTimePitch alloc] init];
        a.rateNode.pitch = 0.0f;  // cents; 0 = preserve source pitch

        [a.engine attachNode:a.playerNode];
        [a.engine attachNode:a.rateNode];
        AVAudioFormat* fmt = file.processingFormat;
        [a.engine connect:a.playerNode to:a.rateNode format:fmt];
        [a.engine connect:a.rateNode to:a.engine.mainMixerNode format:fmt];

        if (![a.engine startAndReturnError:&err]) {
            NSString* msg = err.localizedDescription ?: @"engine start failed";
            if (errOut) *errOut = strdup([msg UTF8String]);
            return 0;
        }

        uintptr_t h = atomic_fetch_add(&gNextVideoAudioHandle, 1);
        a.handle = h;
        a.segmentOffset = 0;
        a.paused = YES;

        // Pre-schedule the full track so the first quiVideoAudioPlay
        // call starts producing samples immediately.
        scheduleFromOffset(a, 0);

        [gVideoLock lock];
        gVideoAudios[@(h)] = a;
        [gVideoLock unlock];
        return h;
    }
}

double quiVideoAudioDuration(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a || !a.audioFile) return 0;
    double sr = a.audioFile.processingFormat.sampleRate;
    if (sr <= 0) return 0;
    return (double)a.audioFile.length / sr;
}

double quiVideoAudioSampleRate(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a || !a.audioFile) return 0;
    return a.audioFile.processingFormat.sampleRate;
}

int quiVideoAudioChannels(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a || !a.audioFile) return 0;
    return (int)a.audioFile.processingFormat.channelCount;
}

void quiVideoAudioPlay(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return;
    if (!a.engine.isRunning) {
        NSError* err = nil;
        if (![a.engine startAndReturnError:&err]) {
            if (mediaDebugEnabled()) NSLog(@"[media] engine resume failed: %@", err);
            return;
        }
    }
    [a.playerNode play];
    a.paused = NO;
    a.hasPlayed = YES;
}

void quiVideoAudioPause(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return;
    // Latch hostTime BEFORE pause so the post-pause read returns
    // a stable value (lastRenderTime keeps moving for a tick after
    // pause is called).
    a.pausedHostTime = quiVideoAudioHostTime(handle);
    [a.playerNode pause];
    a.paused = YES;
}

void quiVideoAudioStop(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return;
    [a.playerNode stop];
    a.segmentOffset = 0;
    a.pausedHostTime = 0;
    a.paused = YES;
    a.hasPlayed = NO;
    // Re-schedule from 0 so the next Play starts cleanly.
    scheduleFromOffset(a, 0);
}

void quiVideoAudioSeekTo(uintptr_t handle, double seconds) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return;
    BOOL wasPlaying = a.playerNode.isPlaying;
    [a.playerNode stop];
    if (!scheduleFromOffset(a, seconds)) {
        // Past EOF — leave stopped with segmentOffset set; HostTime
        // returns the requested offset so VideoView's end-of-stream
        // detection fires.
        a.pausedHostTime = seconds;
        a.paused = YES;
        return;
    }
    a.pausedHostTime = seconds;
    if (wasPlaying) {
        [a.playerNode play];
        a.paused = NO;
    }
}

double quiVideoAudioHostTime(uintptr_t handle) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return 0;
    if (a.paused) return a.pausedHostTime;
    AVAudioTime* nodeTime = a.playerNode.lastRenderTime;
    if (!nodeTime || !nodeTime.isSampleTimeValid) {
        return a.segmentOffset;
    }
    AVAudioTime* playerTime = [a.playerNode playerTimeForNodeTime:nodeTime];
    if (!playerTime || !playerTime.isSampleTimeValid || playerTime.sampleRate <= 0) {
        return a.segmentOffset;
    }
    double played = (double)playerTime.sampleTime / playerTime.sampleRate;
    if (played < 0) played = 0;
    return a.segmentOffset + played;
}

void quiVideoAudioSetVolume(uintptr_t handle, float v) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return;
    if (v < 0) v = 0;
    if (v > 1) v = 1;
    a.playerNode.volume = v;
}

void quiVideoAudioSetRate(uintptr_t handle, float r) {
    QuiVideoAudio* a = lookupVideoAudio(handle);
    if (!a) return;
    // AVAudioUnitTimePitch supports rate in [1/32, 32]; the practical
    // useful range is much narrower. Clamp to [0.25, 4.0] both because
    // extreme values produce unintelligible audio and to keep VideoView's
    // clock SetRate symmetric across audio / wall implementations.
    if (r < 0.25f) r = 0.25f;
    if (r > 4.0f) r = 4.0f;
    a.rateNode.rate = r;
}

void quiVideoAudioClose(uintptr_t handle) {
    @autoreleasepool {
        ensureVideoAudioRegistry();
        [gVideoLock lock];
        QuiVideoAudio* a = gVideoAudios[@(handle)];
        [gVideoAudios removeObjectForKey:@(handle)];
        [gVideoLock unlock];
        if (!a) return;
        [a.playerNode stop];
        [a.engine stop];
        a.audioFile = nil;
        a.playerNode = nil;
        a.rateNode = nil;
        a.engine = nil;
    }
}
