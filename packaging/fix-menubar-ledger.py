#!/usr/bin/env python3
"""Repair Control Center's status-item ledger so Vox's menu bar icon shows.

Diagnose first with `make doctor`. Only run this if doctor reports
"Verdict: BLOCKED" with a DISALLOWED foreign owner.

Background
----------
macOS 15+ Control Center keeps a per-app allow-list for menu bar items in a
sandboxed group preference. When a menu bar app is launched as a *child* of
another app -- a terminal, an IDE, an agent host -- Control Center may record
the child's bundle ID under the parent's `menuItemLocations` as well as under
the child's own record. If that parent is switched OFF in
System Settings > Menu Bar, the child is hidden too, and the child's own
toggle has no effect. "Reset Control Center..." does not clear these foreign
mappings.

What this does
--------------
  1. backs up the ledger to ~/Desktop
  2. removes the target bundle ID from every *other* app's menuItemLocations
  3. ensures the target's own record is isAllowed=True
  4. writes the file back atomically and restarts cfprefsd + ControlCenter

Run from a terminal that has Full Disk Access
(System Settings > Privacy & Security > Full Disk Access).

Usage: fix-menubar-ledger.py [--dry-run] [BUNDLE_ID]
"""
import os
import plistlib
import shutil
import subprocess
import sys
import time

PLIST = os.path.expanduser(
    "~/Library/Group Containers/group.com.apple.controlcenter/"
    "Library/Preferences/group.com.apple.controlcenter.plist"
)
DEFAULT_BUNDLE_ID = "dev.vox.menubar"


def bid(node):
    return (node or {}).get("bundle", {}).get("_0")


def main(argv):
    dry_run = "--dry-run" in argv
    ids = [a for a in argv if not a.startswith("--")]
    target = ids[0] if ids else DEFAULT_BUNDLE_ID

    if not os.access(PLIST, os.R_OK):
        sys.exit(
            f"cannot read {PLIST}\n"
            "Grant this terminal Full Disk Access and retry."
        )

    with open(PLIST, "rb") as f:
        outer = plistlib.load(f)
    raw = outer.get("trackedApplications")
    if raw is None:
        sys.exit("ledger has no trackedApplications key; nothing to repair")
    nested = isinstance(raw, bytes)
    entries = plistlib.loads(raw) if nested else raw

    changed = 0
    skipped = []
    for rec in entries:
        if not isinstance(rec, dict) or "menuItemLocations" not in rec:
            continue
        owner = bid(rec.get("location"))
        allowed = rec.get("isAllowed")
        if not isinstance(allowed, bool):
            # Matches the doctor: an isAllowed we can't read is "unknown",
            # and we never rewrite a record we don't understand.
            skipped.append(owner)
            continue
        if owner == target:
            if not allowed:
                rec["isAllowed"] = True
                changed += 1
                print(f"  {owner}: isAllowed False -> True")
            continue
        before = rec["menuItemLocations"]
        after = [loc for loc in before if bid(loc) != target]
        if len(after) != len(before):
            rec["menuItemLocations"] = after
            changed += 1
            print(f"  {owner} (isAllowed={rec.get('isAllowed')}): removed reference to {target}")

    if skipped:
        print(f"  skipped {len(skipped)} record(s) with missing/non-bool isAllowed: {skipped}")
        print("  (run `make doctor` -- these show as INCONCLUSIVE; not touched here)")

    if not changed:
        print(f"nothing to change -- no foreign references to {target}")
        return

    if dry_run:
        print(f"\n--dry-run: would change {changed} entries; nothing written")
        return

    backup = os.path.expanduser(
        f"~/Desktop/group.com.apple.controlcenter.plist.bak-{time.strftime('%Y%m%d-%H%M%S')}"
    )
    os.makedirs(os.path.dirname(backup), exist_ok=True)
    shutil.copy2(PLIST, backup)
    print(f"\nbackup -> {backup}")

    outer["trackedApplications"] = (
        plistlib.dumps(entries, fmt=plistlib.FMT_BINARY) if nested else entries
    )
    tmp = PLIST + ".tmp"
    with open(tmp, "wb") as f:
        plistlib.dump(outer, f, fmt=plistlib.FMT_BINARY)
    os.replace(tmp, PLIST)
    print(f"wrote {PLIST} ({changed} entries changed)")

    # cfprefsd caches this domain; restart it so the on-disk copy wins, then
    # restart ControlCenter so it re-reads the ledger.
    subprocess.run(["killall", "cfprefsd"], check=False)
    time.sleep(1)
    subprocess.run(["killall", "ControlCenter"], check=False)
    print("restarted cfprefsd + ControlCenter")
    print(f"\nNow launch {target} from Finder or a plain terminal (not from an IDE/agent).")
    print(f"Rollback if needed:  cp '{backup}' '{PLIST}' && killall cfprefsd ControlCenter")


if __name__ == "__main__":
    main(sys.argv[1:])
