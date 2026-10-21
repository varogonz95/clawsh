// Key helpers for the session terminal. Plain script (no build step); also
// loadable from Node for tests.
var clawshKeys = {
  // True for a plain Shift+Enter (no Ctrl, Alt or Meta, not an IME
  // composition), whatever the event type: the keypress and keyup that follow
  // a keydown must be swallowed too, or xterm still sends its own \r.
  isShiftEnter(ev) {
    return ev.key === 'Enter' && !!ev.shiftKey && !ev.isComposing &&
      !ev.ctrlKey && !ev.altKey && !ev.metaKey;
  },
  // True for the keydown of a plain Shift+Enter: the one event that sends.
  shiftEnterNewline(ev) {
    return ev.type === 'keydown' && this.isShiftEnter(ev);
  },
};
if (typeof module !== 'undefined') module.exports = clawshKeys;
