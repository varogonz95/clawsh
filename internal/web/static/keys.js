// Key helpers for the session terminal. Plain script (no build step); also
// loadable from Node for tests.
var clawshKeys = {
  // True for a plain Shift+Enter keydown (no Ctrl, Alt or Meta).
  shiftEnterNewline(ev) {
    return ev.type === 'keydown' && ev.key === 'Enter' && !!ev.shiftKey &&
      !ev.ctrlKey && !ev.altKey && !ev.metaKey;
  },
};
if (typeof module !== 'undefined') module.exports = clawshKeys;
