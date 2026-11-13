package core

// ApplyImage : socle img pour le projet — env.yaml modifié (ou créé) seulement
// si l'environnement fusionné n'a pas déjà ce socle.
func ApplyImage(dir, img string) error {
	if e, err := LoadEnv(dir); err == nil && e.Image == img {
		return nil
	}
	return setEnvImage(dir, img) // absent, sans image valable, ou autre socle
}
