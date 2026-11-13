package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLocalRefusal(t *testing.T) {
	cases := []struct {
		b       BackendConf
		refused bool
	}{
		{BackendConf{Name: "host", Type: "host"}, true},
		{BackendConf{Name: "pod", Type: "podman"}, true},
		{BackendConf{Name: "dock", Type: "docker", Host: "unix:///var/run/docker.sock"}, true},
		{BackendConf{Name: "dd", Type: "docker", Host: "npipe:////./pipe/docker_engine"}, true},
		{BackendConf{Name: "srv", Type: "docker", Host: "tcp://srv1:2375"}, false},
		{BackendConf{Name: "srvssh", Type: "docker", Host: "ssh://moi@srv1"}, false},
		{BackendConf{Name: "ctx", Type: "docker", Context: "srv1"}, false},
		{BackendConf{Name: "pr", Type: "podman", Host: "ssh://moi@srv2/run/user/1000/podman/podman.sock"}, false},
		{BackendConf{Name: "grp", Type: "docker", Hosts: []string{"tcp://a:2375", "tcp://b:2375"}}, false},
		{BackendConf{Name: "grpmix", Type: "docker", Hosts: []string{"tcp://a:2375", "unix:///x.sock"}}, true},
		{BackendConf{Name: "ssh", Type: "ssh", Host: "srv3"}, false},
		{BackendConf{Name: "lab", Type: "k8s"}, false},
	}
	for _, c := range cases {
		p, refused := localRefusal("windows", c.b)
		if refused != c.refused {
			t.Errorf("windows %s : refusé=%v, attendu %v (%v)", c.b.Name, refused, c.refused, p)
		}
		if refused && (!strings.Contains(p.What, "refusé sous Windows") || !strings.Contains(p.Fix, "tcp://")) {
			t.Errorf("message : %+v", p)
		}
		if _, r := localRefusal("linux", c.b); r {
			t.Errorf("linux %s : rien n'est refusé", c.b.Name)
		}
	}
}

func TestToolsForDeclaredBackends(t *testing.T) {
	bs := []BackendConf{
		{Name: "host", Type: "host"},
		{Name: "pool", Type: "docker", Hosts: []string{"tcp://a:2375", "ssh://moi@b"}},
		{Name: "lab", Type: "k8s"},
		{Name: "far", Type: "ssh", Host: "srv3"},
		{Name: "pod", Type: "podman"}, // local : refusé sous Windows, donc pas d'outil
	}
	got := toolsFor("windows", bs)
	want := map[string][]string{"git": {"launcher"}, "gh": {"launcher"}, "docker": {"pool"}, "ssh": {"pool", "far"}, "kubectl": {"lab"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("windows : %v", got)
	}
	if got := toolsFor("linux", bs); !reflect.DeepEqual(got["podman"], []string{"pod"}) {
		t.Errorf("linux : podman local requis : %v", got)
	}
	if got := toolsFor("windows", nil); len(got) != 2 {
		t.Errorf("sans backend : git et gh seuls : %v", got)
	}
}

func TestToolsStatus(t *testing.T) {
	bs := []BackendConf{{Name: "lab", Type: "k8s"}, {Name: "far", Type: "ssh", Host: "srv"}}
	look := func(n string) (string, error) {
		if n == "git" || n == "ssh" {
			return `C:\bin\` + n + ".exe", nil
		}
		return "", errors.New("absent")
	}
	ts := toolsStatus("windows", bs, look)
	var names []string
	for _, s := range ts {
		names = append(names, s.Tool)
	}
	if strings.Join(names, ",") != "ssh,git,kubectl,gh" {
		t.Fatalf("ordre : %v", names)
	}
	miss := MissingTools(ts)
	if len(miss) != 2 || miss[0].Tool != "kubectl" || miss[0].Install != "winget install --id Kubernetes.kubectl -e" ||
		miss[1].Tool != "gh" || !miss[1].Optional || miss[1].Install != "winget install --id GitHub.cli -e" {
		t.Errorf("manquants : %+v", miss)
	}
	if !ts[0].Found || ts[0].Path != `C:\bin\ssh.exe` || ts[0].RequiredBy[0] != "far" {
		t.Errorf("ssh : %+v", ts[0])
	}
	for _, tool := range []string{"ssh", "git", "docker", "podman", "kubectl", "gh"} {
		if InstallCmd("windows", tool) == "" || InstallCmd("linux", tool) == "" {
			t.Errorf("commande d'installation manquante : %s", tool)
		}
	}
	if !strings.HasPrefix(InstallCmd("windows", "docker"), "winget install") {
		t.Error("docker : winget attendu")
	}
}
