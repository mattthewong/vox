#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

// notifySend posts a native macOS notification using NSUserNotification.
// This API is deprecated in macOS 11+ but still works and avoids the
// entitlement requirements of UNUserNotificationCenter. For a developer
// tool that doesn't ship through the App Store, this is the pragmatic choice.
//
// Note: NSUserNotification may silently drop notifications on macOS 13+
// for unsigned/non-bundled CLI tools. This is a known limitation.
void notifySend(const char *title, const char *body) {
    if (title == NULL || body == NULL) return;

    // Copy the C strings into heap buffers so the dispatch block owns the data.
    // Creating NSString inside the block avoids autoreleased objects on cgo
    // threads that lack an @autoreleasepool (matches ui_darwin.m pattern).
    char *tCopy = strdup(title);
    char *bCopy = strdup(body);

    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            NSString *nsTitle = [NSString stringWithUTF8String:tCopy];
            NSString *nsBody  = [NSString stringWithUTF8String:bCopy];
            free(tCopy);
            free(bCopy);

            // Guard against nil from invalid UTF-8.
            if (nsTitle == nil) nsTitle = @"[invalid text]";
            if (nsBody  == nil) nsBody  = @"[invalid text]";

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
            NSUserNotification *notification = [[NSUserNotification alloc] init];
            notification.title = nsTitle;
            notification.informativeText = nsBody;
            notification.soundName = nil; // silent — vox has its own sounds

            [[NSUserNotificationCenter defaultUserNotificationCenter]
                deliverNotification:notification];
#pragma clang diagnostic pop
        }
    });
}
