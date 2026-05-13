#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
#include "_cgo_export.h"

// MARK: - Globals (all main-thread-only)

static NSStatusItem  *statusItem        = nil;
static NSMenu        *statusMenu        = nil;
static NSMenuItem    *statusLineItem    = nil;
static NSMenuItem    *lastTextItem      = nil;
static NSMenuItem    *hotkeyLineItem    = nil;
static NSMenu        *hotkeyPresetsMenu = nil;
static NSMenuItem    *hotkeyPresetsItem = nil;
static NSMenuItem    *pauseItem         = nil;
static NSMenuItem    *modeHoldItem      = nil;
static NSMenuItem    *modeToggleItem    = nil;
static NSMenuItem    *soundsItem        = nil;
static NSMenuItem    *autoPasteItem     = nil;
static NSMenuItem    *aiPostProcessItem = nil;
static NSMenuItem    *promptModeItem    = nil;
static NSMenuItem    *voiceCommandsItem = nil;
static NSMenuItem    *contextAwareItem  = nil;

// Tracks whether the user has paused vox via the menu. We keep it in C
// because applySymbol consults it on every state transition to render a
// "muted" icon when paused regardless of recording/transcribing state.
static BOOL isPaused = NO;

// MARK: - App delegate

@interface VoxAppDelegate : NSObject<NSApplicationDelegate>
- (void)quitClicked:(id)sender;
- (void)showLogClicked:(id)sender;
- (void)hotkeyClicked:(id)sender;
- (void)pauseClicked:(id)sender;
- (void)modeHoldClicked:(id)sender;
- (void)modeToggleClicked:(id)sender;
- (void)soundsClicked:(id)sender;
- (void)autoPasteClicked:(id)sender;
- (void)aiPostProcessClicked:(id)sender;
- (void)promptModeClicked:(id)sender;
- (void)voiceCommandsClicked:(id)sender;
- (void)contextAwareClicked:(id)sender;
@end

@implementation VoxAppDelegate
- (void)quitClicked:(id)sender {
    onQuitClicked();
}
- (void)showLogClicked:(id)sender {
    onShowLogClicked();
}
// The chosen NSMenuItem's representedObject carries the hotkey spec
// (e.g. "option+space") that Go uses to re-parse and install triggers.
- (void)hotkeyClicked:(id)sender {
    NSMenuItem *item = (NSMenuItem *)sender;
    NSString *spec = (NSString *)item.representedObject;
    if (spec == nil) return;
    const char *c = [spec UTF8String];
    onHotkeyChosen((char *)c);
}
// All toggle handlers send the *new* state (1 = on after click, 0 = off).
// We flip locally so Go doesn't have to round-trip just to confirm the
// state change; Go can correct us afterwards via uiSet*State if a write
// fails for some reason.
- (void)pauseClicked:(id)sender {
    onPauseToggled(isPaused ? 0 : 1);
}
- (void)modeHoldClicked:(id)sender {
    onModeChanged(1);
}
- (void)modeToggleClicked:(id)sender {
    onModeChanged(0);
}
- (void)soundsClicked:(id)sender {
    onSoundsToggled(soundsItem.state == NSControlStateValueOn ? 0 : 1);
}
- (void)autoPasteClicked:(id)sender {
    onAutoPasteToggled(autoPasteItem.state == NSControlStateValueOn ? 0 : 1);
}
- (void)aiPostProcessClicked:(id)sender {
    onAIPostProcessToggled(aiPostProcessItem.state == NSControlStateValueOn ? 0 : 1);
}
- (void)promptModeClicked:(id)sender {
    onPromptModeToggled(promptModeItem.state == NSControlStateValueOn ? 0 : 1);
}
- (void)voiceCommandsClicked:(id)sender {
    onVoiceCommandsToggled(voiceCommandsItem.state == NSControlStateValueOn ? 0 : 1);
}
- (void)contextAwareClicked:(id)sender {
    onContextAwareToggled(contextAwareItem.state == NSControlStateValueOn ? 0 : 1);
}
@end

static VoxAppDelegate *appDelegate = nil;

// MARK: - Status bar

// Default macOS menubar SF Symbols render at ~14pt. Bumping to 18pt makes
// the icon noticeably more visible without crowding adjacent menubar items.
static const CGFloat kStatusSymbolPointSize = 18.0;

// Apply an SF Symbol to the status item button. Template=YES so it picks up
// the menubar's foreground color (auto-adapts to light/dark mode). If the
// symbol is unknown on this macOS, we clear the image and fall back to the
// symbol's name as text so something is always visible.
//
// If isPaused is set, we override the requested symbol with "waveform.slash"
// so the menubar makes it visually obvious vox is disabled.
static void applySymbol(NSString *name) {
    NSString *effective = isPaused ? @"waveform.slash" : name;
    NSImage *img = [NSImage imageWithSystemSymbolName:effective
                             accessibilityDescription:@"Vox"];
    if (img == nil) {
        statusItem.button.image = nil;
        statusItem.button.title = effective;
        return;
    }
    NSImageSymbolConfiguration *cfg =
        [NSImageSymbolConfiguration configurationWithPointSize:kStatusSymbolPointSize
                                                        weight:NSFontWeightRegular];
    img = [img imageWithSymbolConfiguration:cfg];
    [img setTemplate:YES];
    statusItem.button.image = img;
    statusItem.button.title = @"";
}

// Remembers the last requested symbol so we can re-render it when pause
// state changes (otherwise toggling pause off would leave the slash icon
// up until the next state transition).
static NSString *lastRequestedSymbol = nil;
static void rememberAndApply(NSString *name) {
    // Retain-new-before-release-old is safe even if name == lastRequestedSymbol.
    [name retain];
    [lastRequestedSymbol release];
    lastRequestedSymbol = name;
    applySymbol(name);
}

void uiInit(const char *hotkeyLabel) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // Accessory means: no Dock icon, no main menu — just a menubar item.
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        appDelegate = [[VoxAppDelegate alloc] init];
        [NSApp setDelegate:appDelegate];

        statusItem = [[[NSStatusBar systemStatusBar]
            statusItemWithLength:NSVariableStatusItemLength] retain];
        rememberAndApply(@"waveform.circle");

        statusMenu = [[NSMenu alloc] init];

        statusLineItem = [[NSMenuItem alloc] initWithTitle:@"Status: Idle"
                                                    action:nil
                                             keyEquivalent:@""];
        [statusLineItem setEnabled:NO];
        [statusMenu addItem:statusLineItem];

        lastTextItem = [[NSMenuItem alloc] initWithTitle:@""
                                                  action:nil
                                           keyEquivalent:@""];
        [lastTextItem setEnabled:NO];
        [lastTextItem setHidden:YES];
        [statusMenu addItem:lastTextItem];

        [statusMenu addItem:[NSMenuItem separatorItem]];

        // --- Pause toggle (top of menu — easy to find) ---
        pauseItem = [[NSMenuItem alloc] initWithTitle:@"Pause Vox"
                                               action:@selector(pauseClicked:)
                                        keyEquivalent:@""];
        [pauseItem setTarget:appDelegate];
        [statusMenu addItem:pauseItem];

        [statusMenu addItem:[NSMenuItem separatorItem]];

        // --- Hotkey row + submenu ---
        NSString *hkLabelStr = hotkeyLabel ? [NSString stringWithUTF8String:hotkeyLabel] : @"";
        NSString *hkLabel = [NSString stringWithFormat:@"Hotkey: %@", hkLabelStr];
        hotkeyLineItem = [[NSMenuItem alloc] initWithTitle:hkLabel
                                                    action:nil
                                             keyEquivalent:@""];
        [hotkeyLineItem setEnabled:NO];
        [statusMenu addItem:hotkeyLineItem];

        hotkeyPresetsItem = [[NSMenuItem alloc] initWithTitle:@"Change Hotkey"
                                                       action:nil
                                                keyEquivalent:@""];
        hotkeyPresetsMenu = [[NSMenu alloc] initWithTitle:@"Change Hotkey"];
        [hotkeyPresetsItem setSubmenu:hotkeyPresetsMenu];
        [statusMenu addItem:hotkeyPresetsItem];

        // --- Mode submenu (radio: Hold to Talk / Toggle) ---
        NSMenuItem *modeItem = [[[NSMenuItem alloc] initWithTitle:@"Mode"
                                                           action:nil
                                                    keyEquivalent:@""] autorelease];
        NSMenu *modeMenu = [[[NSMenu alloc] initWithTitle:@"Mode"] autorelease];
        modeHoldItem = [[NSMenuItem alloc] initWithTitle:@"Hold to Talk"
                                                  action:@selector(modeHoldClicked:)
                                           keyEquivalent:@""];
        [modeHoldItem setTarget:appDelegate];
        modeToggleItem = [[NSMenuItem alloc] initWithTitle:@"Toggle (press to start/stop)"
                                                    action:@selector(modeToggleClicked:)
                                             keyEquivalent:@""];
        [modeToggleItem setTarget:appDelegate];
        [modeMenu addItem:modeHoldItem];
        [modeMenu addItem:modeToggleItem];
        [modeItem setSubmenu:modeMenu];
        [statusMenu addItem:modeItem];

        [statusMenu addItem:[NSMenuItem separatorItem]];

        // --- Inline checkboxes ---
        soundsItem = [[NSMenuItem alloc] initWithTitle:@"Play sounds"
                                                action:@selector(soundsClicked:)
                                         keyEquivalent:@""];
        [soundsItem setTarget:appDelegate];
        [statusMenu addItem:soundsItem];

        autoPasteItem = [[NSMenuItem alloc] initWithTitle:@"Auto-paste transcription"
                                                   action:@selector(autoPasteClicked:)
                                            keyEquivalent:@""];
        [autoPasteItem setTarget:appDelegate];
        [statusMenu addItem:autoPasteItem];

        [statusMenu addItem:[NSMenuItem separatorItem]];

        // --- AI Features ---
        NSMenuItem *aiHeader = [[NSMenuItem alloc] initWithTitle:@"AI Features"
                                                           action:nil
                                                    keyEquivalent:@""];
        [aiHeader setEnabled:NO];
        [statusMenu addItem:aiHeader];

        aiPostProcessItem = [[NSMenuItem alloc] initWithTitle:@"AI post-processing"
                                                        action:@selector(aiPostProcessClicked:)
                                                 keyEquivalent:@""];
        [aiPostProcessItem setTarget:appDelegate];
        [statusMenu addItem:aiPostProcessItem];

        promptModeItem = [[NSMenuItem alloc] initWithTitle:@"Prompt mode"
                                                     action:@selector(promptModeClicked:)
                                              keyEquivalent:@""];
        [promptModeItem setTarget:appDelegate];
        [statusMenu addItem:promptModeItem];

        voiceCommandsItem = [[NSMenuItem alloc] initWithTitle:@"Voice commands"
                                                        action:@selector(voiceCommandsClicked:)
                                                 keyEquivalent:@""];
        [voiceCommandsItem setTarget:appDelegate];
        [statusMenu addItem:voiceCommandsItem];

        contextAwareItem = [[NSMenuItem alloc] initWithTitle:@"Context-aware formatting"
                                                       action:@selector(contextAwareClicked:)
                                                keyEquivalent:@""];
        [contextAwareItem setTarget:appDelegate];
        [statusMenu addItem:contextAwareItem];

        [statusMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *showLogItem = [[[NSMenuItem alloc] initWithTitle:@"Show Log…"
                                                               action:@selector(showLogClicked:)
                                                        keyEquivalent:@""] autorelease];
        [showLogItem setTarget:appDelegate];
        [statusMenu addItem:showLogItem];

        [statusMenu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *quitItem = [[[NSMenuItem alloc] initWithTitle:@"Quit Vox"
                                                           action:@selector(quitClicked:)
                                                    keyEquivalent:@"q"] autorelease];
        [quitItem setTarget:appDelegate];
        [statusMenu addItem:quitItem];

        statusItem.menu = statusMenu;
    }
}

void uiSetSymbol(const char *name) {
    // Copy the C string into a heap buffer so the dispatch block owns the data.
    // Creating NSString inside the block avoids autoreleased objects on cgo
    // threads that lack an @autoreleasepool.
    char *copy = strdup(name);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *s = [NSString stringWithUTF8String:copy];
        free(copy);
        rememberAndApply(s);
    });
}

void uiSetStatusLine(const char *text) {
    char *copy = strdup(text);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *s = [NSString stringWithUTF8String:copy];
        free(copy);
        statusLineItem.title = s;
    });
}

void uiSetLastText(const char *text) {
    char *copy = strdup(text);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *s = [NSString stringWithUTF8String:copy];
        free(copy);
        if ([s length] == 0) {
            [lastTextItem setHidden:YES];
            return;
        }
        NSString *display = s;
        if ([s length] > 50) {
            display = [[s substringToIndex:47] stringByAppendingString:@"…"];
        }
        lastTextItem.title = [@"Last: " stringByAppendingString:display];
        [lastTextItem setHidden:NO];
    });
}

// specSetFromCSV splits a comma-separated hotkey string (e.g. "fn,cmd+shift")
// into a set of trimmed components. Used so multi-hotkey configurations
// light up *every* matching preset, not just nothing (which is what an
// exact-string match against "fn,cmd+shift" would give us).
static NSSet<NSString *> *specSetFromCSV(NSString *csv) {
    if (csv.length == 0) return [NSSet set];
    NSArray<NSString *> *parts = [csv componentsSeparatedByString:@","];
    NSMutableSet<NSString *> *set = [NSMutableSet setWithCapacity:parts.count];
    for (NSString *p in parts) {
        NSString *trimmed = [p stringByTrimmingCharactersInSet:
            [NSCharacterSet whitespaceCharacterSet]];
        if (trimmed.length > 0) [set addObject:trimmed];
    }
    return set;
}

// Populates the "Change Hotkey" submenu. specs[] are the raw values written
// back to disk (e.g. "option+space"); labels[] are what the user sees
// (e.g. "Option+Space"). `current` may be a comma-separated list, in which
// case every matching preset is checked. Replaces any existing presets, so
// it's safe to call repeatedly.
void uiSetHotkeyPresets(const char **specs, const char **labels, int count, const char *current) {
    NSMutableArray<NSString *> *specArr = [NSMutableArray arrayWithCapacity:count];
    NSMutableArray<NSString *> *labelArr = [NSMutableArray arrayWithCapacity:count];
    for (int i = 0; i < count; i++) {
        [specArr addObject:[NSString stringWithUTF8String:specs[i]]];
        [labelArr addObject:[NSString stringWithUTF8String:labels[i]]];
    }
    NSString *curr = current ? [NSString stringWithUTF8String:current] : @"";
    NSSet<NSString *> *currSet = specSetFromCSV(curr);
    dispatch_async(dispatch_get_main_queue(), ^{
        [hotkeyPresetsMenu removeAllItems];
        for (NSUInteger i = 0; i < specArr.count; i++) {
            NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:labelArr[i]
                                                          action:@selector(hotkeyClicked:)
                                                   keyEquivalent:@""];
            [item setTarget:appDelegate];
            [item setRepresentedObject:specArr[i]];
            if ([currSet containsObject:specArr[i]]) {
                [item setState:NSControlStateValueOn];
            }
            [hotkeyPresetsMenu addItem:item];
        }
    });
}

// Updates which submenu items have the checkmark. Called after a successful
// hotkey change so the menu reflects the new active selection. Accepts a
// comma-separated list so multi-hotkey configurations stay in sync.
void uiSetHotkeyCheckmark(const char *spec) {
    char *copy = strdup(spec);
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *s = [NSString stringWithUTF8String:copy];
        free(copy);
        NSSet<NSString *> *set = specSetFromCSV(s);
        for (NSMenuItem *item in hotkeyPresetsMenu.itemArray) {
            BOOL on = [set containsObject:(NSString *)item.representedObject];
            [item setState:on ? NSControlStateValueOn : NSControlStateValueOff];
        }
    });
}

// Updates the disabled "Hotkey: X" info row in the main menu.
void uiSetHotkeyLabel(const char *label) {
    char *copy = label ? strdup(label) : NULL;
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *l = copy ? [NSString stringWithUTF8String:copy] : @"";
        if (copy) free(copy);
        NSString *s = [NSString stringWithFormat:@"Hotkey: %@", l];
        hotkeyLineItem.title = s;
    });
}

// MARK: - Toggle setters

void uiSetPaused(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        isPaused = on ? YES : NO;
        pauseItem.title = isPaused ? @"Resume Vox" : @"Pause Vox";
        // Re-render the current icon under the new pause state.
        if (lastRequestedSymbol != nil) {
            applySymbol(lastRequestedSymbol);
        }
    });
}

// holdToTalk: 1 = "Hold to Talk" gets the dot, 0 = "Toggle" gets the dot.
void uiSetMode(int holdToTalk) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [modeHoldItem   setState:holdToTalk ? NSControlStateValueOn : NSControlStateValueOff];
        [modeToggleItem setState:holdToTalk ? NSControlStateValueOff : NSControlStateValueOn];
    });
}

void uiSetSoundsEnabled(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [soundsItem setState:on ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

void uiSetAutoPaste(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [autoPasteItem setState:on ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

// MARK: - AI Feature setters

void uiSetAIPostProcess(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [aiPostProcessItem setState:on ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

void uiSetPromptMode(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [promptModeItem setState:on ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

void uiSetVoiceCommands(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [voiceCommandsItem setState:on ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

void uiSetContextAware(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [contextAwareItem setState:on ? NSControlStateValueOn : NSControlStateValueOff];
    });
}

// MARK: - Run loop

void uiRun(void) {
    [NSApp run];
}

void uiQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}
