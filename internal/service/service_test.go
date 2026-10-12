package service

import (
	"reflect"
	"strings"
	"testing"
)

var plain = Options{Exe: "/usr/local/bin/clawsh", Claude: "/usr/bin/claude", Listen: ":7420",
	EnvFile: "/home/u/.config/clawsh/agent.env", LogFile: "/home/u/.config/clawsh/agent.log"}

var spaced = Options{Exe: "/opt/my tools/clawsh", Claude: "/opt/my tools/claude", Listen: "127.0.0.1:7420",
	EnvFile: "/home/u/my cfg/agent.env", LogFile: "/home/u/my cfg/agent.log"}

func TestSystemdUnit(t *testing.T) {
	want := `[Unit]
Description=clawsh agent

[Service]
ExecStart=/usr/local/bin/clawsh agent --listen :7420 --claude /usr/bin/claude --env-file /home/u/.config/clawsh/agent.env
Restart=on-failure

[Install]
WantedBy=default.target
`
	if got := SystemdUnit(plain); got != want {
		t.Fatalf("got:\n%s", got)
	}
	got := SystemdUnit(spaced)
	line := `ExecStart="/opt/my tools/clawsh" agent --listen 127.0.0.1:7420 --claude "/opt/my tools/claude" --env-file "/home/u/my cfg/agent.env"`
	if !strings.Contains(got, line+"\n") {
		t.Fatalf("spaces not quoted:\n%s", got)
	}
}

func TestLaunchdPlist(t *testing.T) {
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>dev.clawsh.agent</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/my tools/clawsh</string>
		<string>agent</string>
		<string>--listen</string>
		<string>127.0.0.1:7420</string>
		<string>--claude</string>
		<string>/opt/my tools/claude</string>
		<string>--env-file</string>
		<string>/home/u/my cfg/agent.env</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>/home/u/my cfg/agent.log</string>
	<key>StandardErrorPath</key>
	<string>/home/u/my cfg/agent.log</string>
</dict>
</plist>
`
	if got := LaunchdPlist(spaced); got != want {
		t.Fatalf("got:\n%s", got)
	}
	o := plain
	o.Claude = "/a&b/<claude>"
	if !strings.Contains(LaunchdPlist(o), "<string>/a&amp;b/&lt;claude&gt;</string>") {
		t.Fatal("xml not escaped")
	}
}

func TestSchtasksCreateArgs(t *testing.T) {
	want := []string{"/Create", "/F", "/SC", "ONLOGON", "/RL", "LIMITED", "/TN", "clawsh-agent", "/TR",
		`"C:\Program Files\clawsh\clawsh.exe" agent --detach --listen :7420 --claude "C:\Users\u\bin\claude.exe" --env-file "C:\Users\u\AppData\Roaming\clawsh\agent.env"`}
	o := Options{Exe: `C:\Program Files\clawsh\clawsh.exe`, Claude: `C:\Users\u\bin\claude.exe`, Listen: ":7420",
		EnvFile: `C:\Users\u\AppData\Roaming\clawsh\agent.env`}
	if got := SchtasksCreateArgs(o); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}
