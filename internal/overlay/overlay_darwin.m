#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Overlay window and labels -- global references.
// Only touched on the main queue: every entry point below dispatch_asyncs
// its AppKit work there, so callers may be on any thread.
static NSWindow *overlayWindow = nil;
static NSTextField *textLabel = nil;
static NSTextField *statusLabel = nil;

void overlayInit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (overlayWindow != nil) return;

        // Get main screen dimensions.
        NSRect screenRect = [[NSScreen mainScreen] frame];
        CGFloat width = 500;
        CGFloat height = 60;
        CGFloat x = (screenRect.size.width - width) / 2;
        CGFloat y = 80; // 80px from bottom of screen

        NSRect windowRect = NSMakeRect(x, y, width, height);

        overlayWindow = [[NSWindow alloc]
            initWithContentRect:windowRect
            styleMask:NSWindowStyleMaskBorderless
            backing:NSBackingStoreBuffered
            defer:NO];

        [overlayWindow setLevel:NSFloatingWindowLevel];
        [overlayWindow setBackgroundColor:[NSColor colorWithRed:0.1 green:0.1 blue:0.1 alpha:0.85]];
        [overlayWindow setOpaque:NO];
        [overlayWindow setHasShadow:YES];
        [overlayWindow setIgnoresMouseEvents:YES];
        [overlayWindow setCollectionBehavior:NSWindowCollectionBehaviorCanJoinAllSpaces |
                                              NSWindowCollectionBehaviorStationary];

        // Round corners.
        overlayWindow.contentView.wantsLayer = YES;
        overlayWindow.contentView.layer.cornerRadius = 12;
        overlayWindow.contentView.layer.masksToBounds = YES;

        // Status label (small, top-left).
        statusLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 36, width - 32, 18)];
        [statusLabel setStringValue:@"Listening..."];
        [statusLabel setTextColor:[NSColor colorWithRed:0.6 green:0.8 blue:1.0 alpha:1.0]];
        [statusLabel setFont:[NSFont systemFontOfSize:11 weight:NSFontWeightMedium]];
        [statusLabel setBezeled:NO];
        [statusLabel setDrawsBackground:NO];
        [statusLabel setEditable:NO];
        [statusLabel setSelectable:NO];
        [overlayWindow.contentView addSubview:statusLabel];

        // Text label (main content).
        textLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 8, width - 32, 24)];
        [textLabel setStringValue:@""];
        [textLabel setTextColor:[NSColor whiteColor]];
        [textLabel setFont:[NSFont systemFontOfSize:14 weight:NSFontWeightRegular]];
        [textLabel setBezeled:NO];
        [textLabel setDrawsBackground:NO];
        [textLabel setEditable:NO];
        [textLabel setSelectable:NO];
        // Truncate the head so the newest words stay visible as text grows.
        [textLabel setLineBreakMode:NSLineBreakByTruncatingHead];
        [overlayWindow.contentView addSubview:textLabel];
    });
}

void overlayShow(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (overlayWindow != nil) {
            [textLabel setStringValue:@""];
            [overlayWindow orderFront:nil];
        }
    });
}

void overlayHide(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (overlayWindow != nil) {
            [overlayWindow orderOut:nil];
        }
    });
}

void overlayUpdateText(const char *text) {
    if (text == NULL) return;
    // Copy now: the Go caller frees text as soon as we return.
    char *copy = strdup(text);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *nsText = [NSString stringWithUTF8String:copy];
        free(copy);
        if (nsText == nil) {
            // Invalid UTF-8; replace with a safe fallback.
            nsText = @"[invalid text]";
        }
        if (textLabel != nil) {
            [textLabel setStringValue:nsText];
        }
    });
}

void overlaySetStatus(const char *status) {
    if (status == NULL) return;
    char *copy = strdup(status);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *nsStatus = [NSString stringWithUTF8String:copy];
        free(copy);
        if (nsStatus == nil) {
            nsStatus = @"";
        }
        if (statusLabel != nil) {
            [statusLabel setStringValue:nsStatus];
        }
    });
}

void overlayClose(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (overlayWindow != nil) {
            [overlayWindow orderOut:nil];
            [overlayWindow close];
            overlayWindow = nil;
        }
        textLabel = nil;
        statusLabel = nil;
    });
}
