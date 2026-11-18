// Package htmlmedia makes htmlcss render the HTML media elements, <video>
// and <audio>, on top of package media.
//
// Importing the package registers both tags with htmlcss.RegisterElement.
// Each element becomes a replaced element backed by an Element: the media
// element's state machine (HTML § 4.8.11 — networkState, readyState,
// paused / ended / seeking, current time, volume, rate, the event sequence
// loadstart → durationchange → loadedmetadata → loadeddata → canplay →
// canplaythrough → play → playing → timeupdate … → pause / ended), a
// media.VideoView or media.AudioPlayer doing the decoding, and Chrome-style
// native controls when the `controls` attribute is present.
//
// Bytes come from the document's resource loader: when the htmlcss
// Options.Resources value also implements Loader, media URLs are opened
// through it (a browser plugs in its network stack); otherwise only local
// files load. A script engine drives an element through For(el) and hears
// its events through Element.OnEvent.
//
// Every method of Element runs on the window's UI thread, like the rest of
// htmlcss; loading happens on a background goroutine and lands back on the
// UI thread through Window.PostJob.
package htmlmedia
