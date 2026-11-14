package core

import (
	"fmt"
	"runtime"

	"gopkg.in/yaml.v3"
)

// ── « Ce poste » : hubs.<machine> de backends.yaml en modèle (#149) ───────

// HubServerView : browser_server d'un poste.
type HubServerView struct {
	Listen string   `json:"listen"`
	URL    string   `json:"url"`
	Allow  []string `json:"allow"`
}

// HubUserView : remote_user d'un poste (uid et gid absents : nil).
type HubUserView struct {
	UID  *int   `json:"uid"`
	GID  *int   `json:"gid"`
	Name string `json:"name"`
	Home string `json:"home"`
}

// HubSettings : réglages d'un poste (communs, ou d'un système).
type HubSettings struct {
	Paths         []PathMap     `json:"paths"`
	BrowserPaths  []BrowserPath `json:"browser_paths"`
	BrowserServer HubServerView `json:"browser_server"`
	RemoteUser    HubUserView   `json:"remote_user"`
	HubCache      string        `json:"hub_cache"`
	BrowserDevice string        `json:"browser_device"`
}

// HubOverride : surcharge propre à un système ; Keys : clés qu'elle redéfinit.
type HubOverride struct {
	Keys     []string    `json:"keys"`
	Settings HubSettings `json:"settings"`
}

// HubModel : section de la machine. Hub : clé du fichier, sinon le nom de
// l'ordinateur (Present faux : créée au premier changement). Effective : pour
// chaque clé, la source qui s'applique ici (« » : réglage commun, sinon le
// système de la surcharge).
type HubModel struct {
	Hub       string                 `json:"hub"`
	Present   bool                   `json:"present"`
	Goos      string                 `json:"goos"`
	Settings  HubSettings            `json:"settings"`
	Overrides map[string]HubOverride `json:"overrides"`
	Effective map[string]string      `json:"effective"`
}

func hubSettingsOf(paths []PathMap, bp *[]BrowserPath, bs *BrowserServer, ru *RemoteUserConf, cache, dev string) HubSettings {
	s := HubSettings{Paths: []PathMap{}, BrowserPaths: []BrowserPath{}, BrowserServer: HubServerView{Allow: []string{}},
		HubCache: cache, BrowserDevice: dev}
	if paths != nil {
		s.Paths = paths
	}
	if bp != nil {
		s.BrowserPaths = *bp
	}
	if bs != nil {
		s.BrowserServer = HubServerView{Listen: bs.Listen, URL: bs.URL, Allow: bs.Allow}
		if s.BrowserServer.Allow == nil {
			s.BrowserServer.Allow = []string{}
		}
	}
	if ru != nil {
		s.RemoteUser = HubUserView{UID: ru.UID, GID: ru.GID, Name: ru.Name, Home: ru.Home}
	}
	return s
}

// ReadHubModel : modèle de hubs.<machine> de backends.yaml (contenu vide :
// section absente) vu du système goos.
func ReadHubModel(content []byte, host, goos string) (HubModel, error) {
	m := HubModel{Hub: host, Goos: goos, Overrides: map[string]HubOverride{}, Effective: map[string]string{}}
	m.Settings = hubSettingsOf(nil, nil, nil, nil, "", "")
	for _, k := range hubOSKeys {
		m.Effective[k] = ""
	}
	var f backendsFile
	if err := yaml.Unmarshal(content, &f); err != nil {
		return m, fmt.Errorf("backends.yaml : %w", err)
	}
	key, h, ok := pickHub(f.Hubs, host)
	if !ok {
		return m, nil
	}
	m.Hub, m.Present = key, true
	m.Settings = hubSettingsOf(h.Paths, h.BrowserPaths, h.BrowserServer, h.RemoteUser, h.HubCache, h.BrowserDevice)
	for _, os := range osKeys {
		ov := h.osBlock(os)
		if ov == nil {
			continue
		}
		var keys []string
		add := func(name string, set bool) {
			if set {
				keys = append(keys, name)
			}
		}
		add("paths", ov.Paths != nil)
		add("browser_paths", ov.BrowserPaths != nil)
		add("browser_server", ov.BrowserServer != nil)
		add("remote_user", ov.RemoteUser != nil)
		add("hub_cache", ov.HubCache != "")
		add("browser_device", ov.BrowserDevice != "")
		m.Overrides[os] = HubOverride{Keys: keys, Settings: hubSettingsOf(ov.Paths, ov.BrowserPaths, ov.BrowserServer, ov.RemoteUser, ov.HubCache, ov.BrowserDevice)}
	}
	for _, k := range hubOSKeys {
		m.Effective[k] = ""
	}
	if ov, ok := m.Overrides[osKey(goos)]; ok {
		for _, k := range ov.Keys {
			m.Effective[k] = osKey(goos)
		}
	}
	return m, nil
}

// HubKeyIn : clé de la machine host dans le contenu de backends.yaml (celle
// d'une section existante, insensible à la casse et au domaine), sinon host.
func HubKeyIn(content []byte, host string) string {
	var f backendsFile
	if yaml.Unmarshal(content, &f) == nil {
		if k, _, ok := pickHub(f.Hubs, host); ok {
			return k
		}
	}
	return host
}

// ApplyHubChanges : changements de la section de la machine (at relatif :
// ["paths"], ["windows", "hub_cache"]) appliqués à backends.yaml par Node.
func ApplyHubChanges(content []byte, host string, changes []ConfigChange) ([]byte, error) {
	if host == "" {
		return nil, fmt.Errorf("nom de la machine inconnu")
	}
	key := HubKeyIn(content, host)
	abs := make([]ConfigChange, len(changes))
	for i, c := range changes {
		if len(c.At) == 0 {
			return nil, fmt.Errorf("changement %d : chemin vide", i+1)
		}
		first := c.At[0]
		known := false
		for _, k := range append(append([]string{}, hubOSKeys...), osKeys...) {
			known = known || k == first
		}
		if !known {
			return nil, fmt.Errorf("changement %d : « %s » n'est pas un réglage d'un poste", i+1, first)
		}
		abs[i] = ConfigChange{Op: c.Op, At: append([]string{"hubs", key}, c.At...), Value: c.Value}
	}
	return ApplyConfigChanges(ConfBackends, content, abs)
}

// HubHost : nom de la machine pour les réglages de poste.
func HubHost() string { return hubName() }

// CurrentGOOS : système de ce launcher.
func CurrentGOOS() string { return runtime.GOOS }

// ── env.yaml du launcher en modèle ────────────────────────────────────────

// LauncherEnvModel : env.yaml du launcher (seules clés qu'il accepte).
type LauncherEnvModel struct {
	Packages          []string            `json:"packages"`
	PackagesByManager map[string][]string `json:"packages_by_manager"`
	Setup             []string            `json:"setup"`
	OnStart           []string            `json:"on_start"`
}

// ReadLauncherEnvModel : modèle de l'env.yaml du launcher (contenu vide : vide).
func ReadLauncherEnvModel(content []byte) (LauncherEnvModel, error) {
	e, err := ReadEnvModel(content)
	return LauncherEnvModel{Packages: e.Packages, PackagesByManager: e.PackagesByManager, Setup: e.Setup, OnStart: e.OnStart}, err
}
