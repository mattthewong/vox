#include <ApplicationServices/ApplicationServices.h>
#include "_cgo_export.h"

static CFMachPortRef _tap = NULL;

static CGEventRef hotkey_callback(
    CGEventTapProxy proxy,
    CGEventType type,
    CGEventRef event,
    void *refcon
) {
    if (type == kCGEventTapDisabledByTimeout) {
        if (_tap) CGEventTapEnable(_tap, true);
        return event;
    }

    CGEventFlags flags = CGEventGetFlags(event);
    int64_t keycode = CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);

    GoInt32 consume = goEventCallback(
        (GoUint32)type,
        (GoUint64)flags,
        (GoInt64)keycode
    );

    if (consume == 1) {
        return NULL;
    }
    return event;
}

int checkAccessibility(void) {
    return AXIsProcessTrusted() ? 1 : 0;
}

int startEventTap(void) {
    CGEventMask mask = CGEventMaskBit(kCGEventFlagsChanged) |
                       CGEventMaskBit(kCGEventKeyDown) |
                       CGEventMaskBit(kCGEventKeyUp);

    _tap = CGEventTapCreate(
        kCGSessionEventTap,
        kCGHeadInsertEventTap,
        kCGEventTapOptionDefault,
        mask,
        hotkey_callback,
        NULL
    );

    if (!_tap) return -1;

    CFRunLoopSourceRef source = CFMachPortCreateRunLoopSource(
        kCFAllocatorDefault, _tap, 0
    );
    CFRunLoopAddSource(CFRunLoopGetCurrent(), source, kCFRunLoopCommonModes);
    CGEventTapEnable(_tap, true);
    CFRunLoopRun();

    return 0;
}
