# Immich web project

This project uses the [SvelteKit](https://kit.svelte.dev/) web framework. Please refer to [the SvelteKit docs](https://kit.svelte.dev/docs) for information on getting started as a contributor to this project. In particular, it will help you navigate the project's code if you understand the basics of [SvelteKit routing](https://kit.svelte.dev/docs/routing).

When developing locally, you will run a SvelteKit Node.js server. When this project is deployed to production, it is built as a SPA and deployed as part of [the server project](../server).

## immich-go fork notes

This tree is a fork of the upstream Immich web application at tag
**v3.1.0** (https://github.com/immich-app/immich), extracted from the
monorepo so it builds standalone against the npm-published
`@immich/sdk@3.1.0`. It remains licensed under the **AGPL-3.0** (see
LICENSE in this directory), like the rest of the upstream project.

Local modifications (all under this fork):

- `package.json`: `@immich/sdk` pinned to the npm release instead of the
  pnpm workspace package; `three` added for the photo-sphere peer
  dependency that the monorepo injected via `pnpm-workspace.yaml`.
- `src/lib/components/asset-viewer/DetailPanel.svelte`: per-asset
  refresh button calling the immich-go extension endpoint
  `POST /api/assets/{id}/refresh` (MT Photos-inspired).
- `src/routes/(user)/utilities/duplicates/.../+page.svelte`: "only
  exact duplicates" toggle calling `GET /api/duplicates?exact=true`
  (MT Photos' MD5 filter, immich-go extension).
- `i18n/{en,zh_Hans,zh_Hant}.json`: one new key (`exact_duplicates_only`).
- `embed.go`: Go embed of `build/` so the compiled SPA ships inside the
  immich-go binary (build with `corepack pnpm run build` in this
  directory; the output is committed for CI/Docker builds without Node).

The compiled output in `build/` is derived from this AGPL-3.0 source and
follows the same license.
