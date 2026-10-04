//go:build darwin && cgo
// ^ Advisory for IDE; file is compiled only on darwin because of .m
// + _darwin suffix.

#import <Cocoa/Cocoa.h>
#import <Quartz/Quartz.h> // PDFKit (PDFDocument)
#include <string.h>

// Print a PDF using PDFKit's high-level print operation. Runs modally on the
// main thread via -runOperation, matching the file-dialog pattern in
// dialog_darwin.m (Go's main goroutine is locked to the OS main thread and
// GLFW pumps there). Page size and orientation come from the PDF MediaBox.
int quiPrintPDF(const void* pdfBytes, int length, const char* jobTitle, int showPanel) {
    @autoreleasepool {
        if (pdfBytes == NULL || length <= 0) return 1;
        NSData* data = [NSData dataWithBytes:pdfBytes length:(NSUInteger)length];
        PDFDocument* doc = [[PDFDocument alloc] initWithData:data];
        if (doc == nil || [doc pageCount] == 0) return 1;

        NSPrintInfo* info = [NSPrintInfo sharedPrintInfo];
        info.horizontalPagination = NSPrintingPaginationModeAutomatic;
        info.verticalPagination = NSPrintingPaginationModeAutomatic;

        NSPrintOperation* op = [doc printOperationForPrintInfo:info
                                                   scalingMode:kPDFPrintPageScaleNone
                                                    autoRotate:YES];
        if (op == nil) return 2;

        if (jobTitle && *jobTitle) {
            op.jobTitle = [NSString stringWithUTF8String:jobTitle];
        }
        BOOL panel = (showPanel != 0);
        op.showsPrintPanel = panel;
        op.showsProgressPanel = panel;

        // Foreground the app so the panel isn't hidden behind the window.
        [NSApp activateIgnoringOtherApps:YES];

        BOOL ok = [op runOperation];
        return ok ? 0 : 2;
    }
}
