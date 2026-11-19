# Contraintes de conception

> Contraintes durables issues des refus/corrections de l'utilisateur, par composant.
> Le planner les respecte, QA les vérifie. Tenu par le CDP.

## team_card
- Les versions (template, claude) ne s'affichent qu'une fois, dans le titre de la carte ; jamais sur chaque ligne. _(v6.2.0, versions_entete)_
- « modèle · effort » reste affiché sur chaque ligne : ne pas le reléguer à l'infobulle. _(v6.2.0, versions_entete — « noms seuls » écartée)_
- Un écart de version se signale par un ⚠ orange en bout de ligne, sans écrire la version sur la ligne. _(v6.2.0, versions_entete — « écart affiché sur la ligne » écartée)_
- Le masquage des inactifs par projet se règle par une icône œil à trois états au bout du titre : ouvert = no hide, fermé = auto hide, grisé = default (suit le réglage global). Pas de bandeau ni de ligne supplémentaire sous le titre, pas d'interrupteur, pas de puce cliquable. _(v6.5.0, masquage_par_projet — A, B, D, E, F écartées)_
- Vocabulaire des trois états : « auto hide », « default », « no hide ». _(v6.5.0, masquage_par_projet)_

## pane_header
- L'état actif / inactif colore tout le bandeau (vert / gris sombre), pas seulement une pastille ou un liseré. _(v6.2.0, etat_activite)_
- La couleur d'état ne dépend pas de la couleur du projet. _(v6.2.0, etat_activite — « teinte du projet » écartée)_
- Coque Linux : une ligne de titre par pane (nom · état). _(v6.2.0, etat_activite)_

## tab_bar
- Teamleaders est isolé à gauche, séparé des onglets de projets, et ne défile pas avec eux. _(v6.2.0, teamleaders_separe)_
- Teamleaders garde son nom (pastille nommée) tant que les onglets de projets tiennent dans la barre ; il se réduit à son icône dès qu'ils débordent. Pas d'icône seule permanente, pas de compteur. _(v6.2.0, teamleaders_separe)_

## project_menu
- L'avancement d'un rafraîchissement s'affiche en bandeau d'étapes en tête des cartes du projet : pas d'indicateur limité à la ligne de l'arbre et au pied de volet, pas de notification flottante. _(v6.2.0, rafraichir_forcer)_
- La fermeture forcée se confirme dans une boîte de dialogue qui liste ce qui sera nettoyé, pas par une question en pied de volet. _(v6.2.0, rafraichir_forcer)_
- Coque Linux : pas de nouvelle touche ; F5 et Suppr sont étendus. _(v6.2.0, rafraichir_forcer)_

## project_layout
- Deux organisations seulement : leader en haut (bandeau de teammates) et leader à gauche (teammates en une puis deux colonnes). _(v6.2.0, organisations)_
- Le choix est un réglage global avec exception par projet ; pas de bascule à la volée par raccourci. _(v6.2.0, organisations)_
- Pas de mise en page libre : un redimensionnement à la main n'est pas conservé. _(v6.2.0, organisations)_
