package sshclient

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/config"
)

func TestCommandUsesConfiguredIdentityWithoutForwarding(t *testing.T) {
	target := config.Server{Name: "mele", SSH: &config.SSHConfig{Host: "prod", User: "root", Port: 2222, IdentityFile: "/a key"}}
	cmd, err := Command(context.Background(), target, true, "")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"-t", "ClearAllForwardings=yes", "PermitLocalCommand=no", "-p 2222", "-l root", "-i /a key", "-- prod"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %v", want, cmd.Args)
		}
	}
	if !reflect.DeepEqual(cmd.Args[len(cmd.Args)-2:], []string{"--", "prod"}) {
		t.Fatal("shell session includes a remote command")
	}
}

func TestCommandRejectsDirectServerAndHonorsSSHConfigPort(t *testing.T) {
	if _, err := Command(context.Background(), config.Server{Name: "direct"}, true, ""); err == nil {
		t.Fatal("direct server accepted")
	}
	cmd, err := Command(context.Background(), config.Server{SSH: &config.SSHConfig{Host: "alias"}}, false, "echo ok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "-p ") {
		t.Fatal("overrode ssh_config port")
	}
	if cmd.Args[len(cmd.Args)-1] != "echo ok" {
		t.Fatal("remote command missing")
	}
	if Quote("a'b") != "'a'\"'\"'b'" {
		t.Fatal("unsafe remote quoting")
	}
}
