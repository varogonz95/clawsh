# How it works

## Sessions

`clawsh new` sends a create request to the agent. The agent resolves the directory, starts the `--claude` executable in a new PTY (creack/pty on Unix, ConPTY on Windows) at your terminal size, and gives the session a random 8-hex-digit ID.

Each session has two loops:

- **Read loop:** reads PTY output, appends it to a bounded scrollback buffer (2 MiB by default, keeping the newest bytes), and hands each chunk to every attached viewer.
- **Wait loop:** waits for the process to exit, records the exit code, and closes the PTY.

A session lives until its process exits or you `kill` it, regardless of whether anyone is attached. Exited sessions stay in `ls` as `exited(code)` until killed.

## Status

Claude Code hooks run `clawsh hook <Event>`, which posts to the agent's `POST /v1/hooks`. The agent maps the event to `working`, `idle` or `needs_input` on the session named by `CLAWSH_SESSION_ID`. Before the first hook a live session is `running`.

## Attaching

`clawsh attach` opens a WebSocket to the agent, puts your terminal in raw mode, and then:

1. The agent sends the scrollback so far, then streams live output. It takes the snapshot and subscribes in one step, so nothing is lost or repeated at the seam.
2. Your keystrokes go to the PTY. The hub checks your terminal size every 250 ms and sends a resize when it changes (Windows has no resize signal, so polling works everywhere).
3. On the first resize, the agent briefly sets the PTY one row shorter and then back. Replayed scrollback rarely matches your screen; the size change makes claude repaint cleanly.
4. **Ctrl-]** detaches. If the process exits, the agent sends its exit code and the hub prints it.

Several viewers can attach to the same session at once.

## Slow viewers

A viewer that can't keep up (its 256-chunk buffer fills) is disconnected instead of slowing the session. Re-attaching replays from scrollback.

## Stopping

`kill` sends a graceful stop (SIGTERM to the process group on Unix), waits 3 s, then forces it. Stopping the agent does the same for every session: sessions don't survive an agent restart. Run `claude --resume` (`clawsh new host -- --resume`) to pick the conversation back up.

For the wire format, see [Protocol](Protocol.md).
