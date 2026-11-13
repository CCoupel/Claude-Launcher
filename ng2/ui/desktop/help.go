package desktop

// Aide (F1) de l'interface native : raccourcis équivalents à ceux de la coque
// tmux, et légende de toutes les icônes (mêmes significations que core/legend.go).

type helpRow struct {
	Key  string `json:"key"`
	Desc string `json:"desc"`
}

type helpSection struct {
	Title string    `json:"title"`
	Rows  []helpRow `json:"rows"`
}

type legendRow struct {
	Icon string `json:"icon"`
	Tone string `json:"tone"`
	Bold bool   `json:"bold,omitempty"`
	Text string `json:"text"`
}

func helpData() map[string]any {
	return map[string]any{
		"sections": []helpSection{
			{"Navigateur", []helpRow{
				{"↑ ↓ PgUp PgDn, molette, clic", "sélectionner un projet (clic sur un projet ouvert : son onglet)"},
				{"Entrée, double-clic", "ouvrir / rejoindre le projet ; les questions (template, reprise) se posent dans son terminal"},
				{"Ctrl+clic, Suppr", "fermer le projet : /exit à claude, puis arrêt du conteneur (confirmation o/N)"},
				{"frappe", "filtrer ; Esc efface le filtre"},
				{"clic hors projet", "onglet ⌗ Teamleaders"},
				{"double-clic catégorie ou racine", "créer un projet dans ce dossier"},
				{"✦ Nouveau projet", "créer un projet à la racine"},
				{"Tab", "passer du navigateur aux terminaux et retour"},
				{"Ctrl+← Ctrl+→, bordure", "largeur du navigateur (mémorisée)"},
				{"Ctrl+R", "rescanner les projets"},
				{"F2", "reconfigurer l'exécution du projet fermé (socle, backend)"},
				{"F5", "mises à jour, consommation et liste des projets maintenant"},
				{"Ctrl+U, clic sur ✕", "masquer les annonces de mise à jour"},
				{"clic sur l'annonce verte", "installer la nouvelle version (l'application redémarre, projets conservés)"},
				{"clic sur l'annonce jaune en échec", "réessayer le téléchargement"},
				{"clic sur l'alerte connexion", "onglet login qui lance claude /login"},
				{"Ctrl+D", "diagnostic des backends (claude-launcher-ng2 backends)"},
				{"Esc", "quitter : l application seule (projets gardés) · t puis o tout fermer"},
				{"F1", "cette aide"},
			}},
			{"Terminaux des projets", []helpRow{
				{"clic sur un pane", "focus clavier sur cet agent"},
				{"double-clic sur l'en-tête d'un pane", "plein écran, double-clic à nouveau pour revenir"},
				{"Ctrl+B z", "idem au clavier"},
				{"Ctrl+B R", "remettre en ordre les panes de la team"},
				{"Ctrl+B Ctrl+B", "envoyer Ctrl+B à l'agent"},
				{"Ctrl+Maj+W", "détacher l'onglet (la session continue dans tmux)"},
				{"Ctrl+Tab, Ctrl+Maj+Tab", "onglet suivant / précédent"},
			}},
		},
		"legend": []legendRow{
			{"○", "dim", false, "rien ne tourne"},
			{"●", "blue", true, "conteneur en marche sans claude"},
			{"▲", "green", true, "ouvert ici, ce launcher a la main"},
			{"●", "green", true, "ouvert ici en lecture seule (un autre launcher a la main)"},
			{"■", "orange", true, "piloté par un autre hub"},
			{"●", "orange", true, "claude tourne, aucun launcher ne le pilote"},
			{"✕", "red", true, "problème : backend inutilisable, config invalide, pod en échec, reprise"},
			{"●", "green", true, "local : au repos sur la branche principale"},
			{"✎", "yellow", true, "local : branche, non commité"},
			{"◆", "cyan", true, "local : branche, tout commité"},
			{"!", "red", true, "local : modifié directement sur la branche principale"},
			{"○", "blue", true, "remote : jamais poussé"},
			{"↑N", "yellow", true, "remote : N commits non poussés"},
			{"⇡", "cyan", true, "remote : poussé, non mergé"},
			{"✓", "green", true, "remote : à jour / mergé"},
			{"↓N", "red", true, "remote : en retard de N commits"},
			{"✔", "green", true, "template à jour"},
			{"⊘", "dim", false, "non géré par template"},
			{"⚠", "yellow", false, "template en retard (/init-project proposé à l'ouverture)"},
			{"⚠", "red", true, "fichiers du template manquants"},
			{"⬡", "dim", false, "environnement conteneur présent / conteneur pas encore créé"},
			{"⬢", "cyan", true, "conteneur en marche"},
			{"⬢", "dim", false, "conteneur arrêté (redémarré à l'ouverture)"},
			{"⚑", "yellow", false, "milestone en cours"},
			{"⚑", "orange", false, "milestone terminé, à publier"},
			{"⚑", "dim", false, "aucun milestone en cours"},
			{"⚑", "green", false, "dernier milestone publié"},
			{"⇄N", "purple", false, "N pull requests ouvertes"},
			{"↺", "yellow", false, "session non close : reprise proposée à l'ouverture (rouge : crash)"},
		},
	}
}
