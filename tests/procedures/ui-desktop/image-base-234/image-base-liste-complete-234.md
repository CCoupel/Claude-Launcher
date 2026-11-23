# #234 — Image de base : liste complète des images (réglages projet)

Projets factices uniquement (règles `COMMON.md` section 15) ; ne jamais ouvrir un vrai projet.

**Préparation** : projet factice en backend conteneur, `env.yaml` avec une image déjà configurée ;
`backends.yaml` factice avec `images:` de 3 entrées au moins. Projet **fermé**.

## Scénario A — fenêtre native (Windows)
1. Clic droit sur le projet → « ⚙ Réglages du projet… » → page Projet, bloc Exécution.
2. Ouvrir la liste « Image de base » (image déjà configurée pré-sélectionnée).
- Attendu : toutes les images de `backends.yaml` + les défauts + l'image courante, pas seulement l'image courante.
3. Choisir une autre image, puis « Autre… » et saisir une image libre → l'état modifié est enregistrable.

## Scénario B — `--web`
Même scénario via le binaire Linux en `--web` (Chrome) : même liste complète.

## Scénario C — projet ouvert
Le champ reste désactivé (verrou backend/image) ; la valeur est affichée.
