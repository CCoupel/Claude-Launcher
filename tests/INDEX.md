# Index des tests

> Tests de specification ecrits par le test-writer, ranges en lots `<famille>/<theme>/<lot>/` ; statuts tenus par le CDP.
> Le chemin porte l'identite ; cet index ne porte que l'etat : une ligne par lot (chemin termine par `/`),
> plus des lignes fichier pour les exceptions (quarantaine). Lot sans ligne = `feature`.
> Convention : `context/COMMON.md` section 15. Statuts : `feature` | `regression` | `quarantaine`.
> Tags : `smoke`, `critical`, `slow`.

| Chemin | Niveau | Composant | Feature | Statut | Tags |
|--------|--------|-----------|---------|--------|------|
| tests/procedures/ui-desktop/taille-caracteres/ | procedure | ui-desktop | Taille des caracteres des terminaux, Ctrl+molette (#140) | feature | |
| tests/procedures/ui-desktop/panes-caches/ | procedure | ui-desktop | Teammates inactifs caches, carte TEAM (#136) | feature | critical |
| tests/integration/ui-tmux/panes-caches/ | integration | ui-tmux | Technique des panes caches : break-pane/join-pane, %id conserves, reserve (#136) | feature | critical |
| tests/procedures/ui-desktop/config-backends/ | procedure | ui-desktop | Reglages : Backends, Pools, Defauts (#118) | feature | |
| tests/procedures/ui-desktop/reglages-projet/ | procedure | ui-desktop | Menu contextuel et reglages d'un projet (#148) | feature | |
| tests/procedures/ui-desktop/config-poste-launcher/ | procedure | ui-desktop | Reglages : Ce poste, Launcher, Conteneurs (#149) | feature | |
| tests/procedures/ui-desktop/copier-coller/ | procedure | ui-desktop | Selection, copier / coller dans les terminaux (#155) | feature | |
| tests/procedures/ui-desktop/taille-zones-150/ | procedure | ui-desktop | Taille des caracteres par zone : arbre, cartes, terminaux (#150) | feature | |
| tests/procedures/ui-desktop/rafraichir-forcer-158/ | procedure | ui-desktop | Rafraichir un projet, Forcer la fermeture : menu, bandeau, pod supprime (#158) | feature | critical |
| tests/procedures/ui-tmux/rafraichir-forcer-158/ | procedure | ui-tmux | Coque Linux : F5 relit le projet, Suppr propose la fermeture forcee (#158) | feature | |
| tests/procedures/ui-desktop/team-lisible-151-152-162/ | procedure | ui-desktop | Carte TEAM (versions en en-tete), en-tetes de panes par etat, onglet Teamleaders isole (#151, #152, #162) | feature | |
| tests/procedures/ui-tmux/team-lisible-151-152/ | procedure | ui-tmux | Coque Linux : carte TEAM, ligne de titre par pane, deux launchers (#151, #152) | feature | |
| tests/procedures/ui-desktop/interface-web-163/ | procedure | ui-desktop | Interface web servie par la fenetre native : reglage a chaud, jeton, hors boucle locale, presse-papiers (#163) | feature | critical |
| tests/procedures/ui-desktop/organisations-153/ | procedure | ui-desktop | Organisations de la fenetre : leader a gauche, exception par projet, panes caches (#153) | feature | |
| tests/procedures/ui-tmux/organisations-153/ | procedure | ui-tmux | Coque Linux : leader a gauche, PROJECT_LAYOUTS, Ctrl+B R (#153) | feature | |
| tests/procedures/ci/ci-60s-179/ | procedure | main | CI sous 60 s : trois passes de scripts/ci.sh, nombre de tests et couverture (#179) | feature | slow |
| tests/procedures/ui-desktop/serveurs-web-183/ | procedure | ui-desktop | Reglages : deux serveurs web separes, commutateur de l'interface web (#183) | feature | |
| tests/procedures/ui-desktop/proxy-https-171/ | procedure | ui-desktop | Interface web derriere un proxy HTTPS : endurance 1 h, session expiree, hote/origine, stabilisation #186/#187 (#171) | feature | slow |
| tests/procedures/ui-desktop/racine-base-165/ | procedure | ui-desktop | Racine de base configurable CL_ROOT_BASE / --root-base, demarrage Windows (#165) | feature | |
| tests/procedures/ci/image-launcher-167/ | procedure | main | Image du launcher du cluster : non root, version, interface servie avec dossier monte, arret propre, QUALIF k8s (#167, #168) | feature | slow |
| tests/integration/helm/ | integration | main | Chart Helm : lint, rendu contrôlé, aucun ClusterRole, ClusterIP, liste d'adresses (Traefik/nginx), TLS obligatoire, variables et sécurité du pod (#170, #173) | feature | |
| tests/procedures/ci/chart-qualif-170/ | procedure | main | Chart en QUALIF : namespace separe, liste d'adresses (test negatif hors plage, depuis un pod, X-Forwarded-For), droits, desinstallation (#170, #173) | feature | slow |
| tests/procedures/ci/qualif-cluster-177/ | procedure | main | QUALIF de bout en bout du launcher du cluster : CI/image, installation, Ingress autorise/refuse, projet factice, pod recree, cohabitation, 1 h, mise a jour par tag, desinstallation, isolation (#177) | feature | slow |
| tests/integration/ci/ci-image-chart-177/ | integration | main | Garde de scripts/ci.sh : outil manquant = echec explicite, etapes chart et image mesurees, budgets D1, une seule image ci gardee (#177) | feature | slow |
| tests/procedures/ci/install-guide-185/ | procedure | main | Installation par Helm en suivant le guide seul, sur une autre racine : prerequis, installation, acces sans jeton, mise a jour/retour arriere, limites dites, desinstallation, releve des ecarts (#185) | feature | slow |
| tests/procedures/ui-desktop/masquage-inactifs-188/ | procedure | ui-desktop | Commutateur du masquage des teammates inactifs : defaut v6.2.0, desactive (tout reapparait, delai conserve, double-clic refuse), puce de la carte TEAM, coque tmux (#188) | feature | |
| tests/procedures/ui-desktop/version-template-199/ | procedure | ui-desktop | Version du template juste, ⚠ carte TEAM, alerte quota GitHub (#199) | feature | |
| ng2/core/tplversion199_test.go | unitaire | core | Comparaisons de versions, carte TEAM, verification GitHub : jeton, ETag, quota, resultat partage (#199) | feature | |
| tests/procedures/ui-web/cles-ssh-backends-194/ | procedure | ui-web | Cles ssh des backends distants depuis le pod : generer/coller, empreinte confirmee, Tester ssh/podman/docker (#194) | feature | |
| ng2/ui/desktop/secrets194_test.go | unitaire | ui-desktop | Coffre des secrets, paire ssh, phrase de passe refusee, known_hosts confirme, journal sans contenu, ssh avec la cle du coffre (#194) | feature | |
| ng2/chart_secrets194_test.go | integration | chart | PVC du coffre, CL_SECRETS_DIR, aucun droit secrets, Secret TLS docker facultatif, clients dans l image (#194) | feature | |
| tests/procedures/ui-web/kubeconfig-backend-193/ | procedure | ui-web | Kubeconfig par backend k8s depuis le pod, namespace jetable, fichier illisible sans repli (#193) | feature | |
| ng2/core/kubeconfig193_test.go | unitaire | core | Priorite kubeconfig > ServiceAccount > KUBECONFIG, --kubeconfig/--context, illisible sans repli, Tester (#193) | feature | |
| ng2/ui/desktop/kubeconfig193_test.go | unitaire | ui-desktop | Depot du kubeconfig dans le coffre : contextes listes, jamais relu ni journalise, refus motives (#193) | feature | |
| ng2/chart_kubeconfig193_test.go | integration | chart | rbac.extraNamespaces et backends.kubeconfigs en lecture seule 0444 (#193) | feature | |
| tests/procedures/ui-web/version-serveur-198/ | procedure | ui-web | Version du serveur : bandeau Recharger sans F5, redemarrage simple, annonce non cliquable en cluster (#198) | feature | |
| ng2/ui/desktop/server198_test.go | unitaire | ui-desktop | server{version,boot} dans init et instantane, boot stable/different, logique client node (#198) | feature | |
| tests/procedures/ui-web/onglets-189-191/ | procedure | ui-web | Onglets : couleur hors selection, titre et largeur stables, ordre par glisser-deposer (#189, #190, #191) | feature | |
| ng2/ui/desktop/tabs189_test.go | unitaire | ui-desktop | Simulation node des onglets : tabView, tabOrder, tabMove, cl.tabOrder, Ctrl+Tab, machine du glisser (#189-#191) | feature | |
| tests/procedures/ui-web/fichiers-174/ | procedure | ui-web | /files : maquette dans Chrome par l adresse interne, rien hors racine, hub inchange (#174) | feature | |
| tests/procedures/ci/networkpolicy-200/ | procedure | chart | NetworkPolicy par defaut, pod tiers refuse, Internet 403, ingress externe refuse (#200) | feature | slow |
| ng2/ui/desktop/files174_test.go | integration | ui-desktop | Route /files : racines, .., liens, CSP sandbox, Host/Sec-Fetch-Site, modes token et none, appariement (#174) | feature | |
| ng2/core/browserfiles174_test.go | unitaire | core | CL_BROWSER_PATH_MAP/HINT en cluster vers CL_WEB_URL/files, hub inchange (#174) | feature | |
| ng2/chart_networkpolicy200_test.go | integration | chart | NetworkPolicy par defaut/desactivable/namespaces, ingress externe refuse, delai d arret (#200) | feature | |
| tests/procedures/ci/fiabilisation-lot1-203-204-207-208/ | procedure | main | Fiabilite LOT-1 : SSE gelee -count=50 (#203), jeton obsolete -count=20 -p 8 (#208), sse_flow init lent (#207), chart = branche (#204) | feature | slow |
| tests/integration/ci/chart-version-204/ | integration | main | Controle Chart.yaml = branche milestone dans scripts/ci.sh : juste OK, faux KO, hors milestone ignore (#204) | feature | |
| tests/procedures/ui-desktop/coffre-partage-201/ | procedure | ui-desktop | Coffre partage : cle saisie sous Windows utilisee par hub et pod, droits 9P, migration PVC claude-qualif (#201) | feature | slow |
| tests/procedures/ui-desktop/arret-propre-205-206/ | procedure | ui-desktop | Arret propre : vidage des canaux apres echeance, .tmp orphelins balayes, exe Windows sur wsl.localhost sans handle residuel (#205, #206) | feature | slow |
| tests/procedures/ui-desktop/masquage-projet-209/ | procedure | ui-desktop | Masquage des teammates inactifs par projet : oeil 3 etats, PROJECT_HIDE_IDLE, teamToggle refuse (#209) | feature | |
| tests/procedures/ui-desktop/cartes-rafraichies-210/ | procedure | ui-desktop | Cartes : signal pendant une relecture lente jamais perdu, fusion, pas de chevauchement, arret, #130 (#210) | feature | slow |
| tests/procedures/config/ecriture-atomique-217/ | procedure | ui-desktop | config.conf atomique : editeur ouvert sous Windows, balayage des temporaires (#217) | feature | |
| tests/integration/config/ecriture-atomique-217/ | integration | core | Ecriture atomique de config.conf : crash simule, retry Windows, balayage, 0600 — ng2/core/atomic217_test.go, ng2/platform/replace217_test.go, tag atomic217 (#217) | feature | |
| tests/procedures/ui-desktop/relancer-launcher-219/ | procedure | ui-desktop | Relancer le launcher : exe Windows, hub reel (vignettes, layout-watch), pod QUALIF (#219) | feature | critical |
| tests/integration/ui-tmux/relancer-launcher-219/ | integration | ui-tmux | Relance de la coque sans perte de session, processus anciens arretes, autre serveur intact — ng2/ui/tmux/relaunch219_test.go (#219) | feature | critical |
| tests/integration/core/relancer-launcher-219/ | integration | core | Processus anciens, binaire change, version installee, relance Dispatch — ng2/core/relaunch219_test.go, ng2/ui/desktop/relaunch219_test.go (#219) | feature | |
| tests/procedures/ui-desktop/sauvegarde-exe-218/ | procedure | ui-desktop | Sauvegarde de l'exe avant mise a jour, retour arriere manuel, sauvegarde verrouillee (#218) | feature | |
| tests/integration/platform/sauvegarde-exe-218/ | integration | platform | Sauvegarde .bak-v<version>, une seule gardee, echec restaure l'original — ng2/platform/selfreplace218_test.go, ng2/core/backup218_test.go, tag backup218 (#218) | feature | |
