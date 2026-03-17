#import <AppKit/AppKit.h>

void *clipboardSnapshotCreate(void) {
    @autoreleasepool {
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        NSArray<NSPasteboardItem *> *items = [pasteboard pasteboardItems];
        if (items == nil) {
            items = @[];
        }

        // Deep-copy pasteboard items so restoration is independent of later clipboard writes.
        NSMutableArray<NSPasteboardItem *> *snapshot = [[NSMutableArray alloc] initWithCapacity:[items count]];
        for (NSPasteboardItem *item in items) {
            NSPasteboardItem *itemCopy = [[NSPasteboardItem alloc] init];
            for (NSPasteboardType type in [item types]) {
                NSData *data = [item dataForType:type];
                if (data != nil) {
                    [itemCopy setData:data forType:type];
                }
            }
            [snapshot addObject:itemCopy];
            [itemCopy release];
        }

        return snapshot; // retained; caller must release via clipboardSnapshotFree
    }
}

int clipboardSnapshotRestore(void *snapshotPtr) {
    if (snapshotPtr == NULL) {
        return 0;
    }

    @autoreleasepool {
        NSArray<NSPasteboardItem *> *snapshot = (NSArray<NSPasteboardItem *> *)snapshotPtr;
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        [pasteboard clearContents];

        if ([snapshot count] == 0) {
            return 1;
        }

        return [pasteboard writeObjects:snapshot] ? 1 : 0;
    }
}

void clipboardSnapshotFree(void *snapshotPtr) {
    if (snapshotPtr == NULL) {
        return;
    }

    @autoreleasepool {
        [(id)snapshotPtr release];
    }
}
