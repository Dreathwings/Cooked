# Cooked

Application web Go qui rassemble et affiche les recettes du site hfresh.info, avec une vue “semaine”, une liste de courses ajustable et un mode hors‑ligne minimal via des recettes intégrées.

## Fonctionnalités

- **Scraping en direct** : récupération des recettes via la sitemap/JSON‑LD de hfresh.info (fallback sur les trois recettes intégrées si le réseau est indisponible).
- **Navigation claire** :
  - `/recettes` : liste complète avec recherche texte, filtrage par régime/tag, difficulté ou mot‑clé.
  - `/recettes/{id}` : fiche détail avec ingrédients et étapes.
  - `/semaine` : les recettes marquées pour la semaine (premières recettes récupérées).
  - `/courses` : liste de courses construite à partir des recettes sélectionnées.
- **Liste de courses dynamique** : ajout, suppression et mise à jour des portions ; quantités proportionnellement recalculées.
- **Rafraîchissement manuel** : bouton “Actualiser depuis hfresh” pour relancer le scraping.
- **Templates prêts à l’emploi** : UI sobre en Go HTML templates, styles embarqués.

## Prérequis

- Go **1.21** ou supérieur.
- Accès réseau vers `https://hfresh.info` si vous souhaitez récupérer les recettes en direct.

## Lancement rapide

```bash
go run ./...
```

Le serveur démarre sur [http://localhost:3044](http://localhost:3044). En cas d’échec du scraping, les recettes intégrées sont utilisées automatiquement.

## Structure des fichiers

- `main.go` : application HTTP, logique métier, scraping JSON‑LD, redimensionnement des ingrédients et données intégrées de secours.
- `templates/*.gohtml` : base layout, listes, détails de recette, vue semaine et liste de courses.

## Flux d’utilisation

1. Ouvrir `/recettes` pour parcourir et filtrer les recettes ; ajouter celles souhaitées à la liste de courses.
2. Ajuster les portions ou retirer des recettes via `/courses`.
3. Consulter les plats recommandés de la semaine sur `/semaine`.
4. Cliquer sur “Actualiser depuis hfresh” pour recharger les données (une limite de 48 recettes est appliquée).

## Développement

Aucun test automatisé n’est fourni ; pour vérifier la compilation :

```bash
go test ./...
```
