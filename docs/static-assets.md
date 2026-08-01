# Static assets: TypeScript, styles, images

Assets go through webpack. The generator reads webpack's manifest and turns it into `<ssr:assets/>` tags and,
for a production build, into files embedded in the binary.

## The pipeline

```
pages/**/index.ts        ─┐
pages/**/styles.scss      │  webpack  ──►  <webDir>/static/…  hashed filenames
pages/**/__ssr_gen__.ts   │            ──►  webpack-assets.json
pages/**/*.png            ─┘
                                            │
                          go-ssr reads the manifest
                                            │
                     <ssr:assets/> tags, <img src> rewriting,
                     pages/static_embed/ + ssrstaticfiles_gen.go
```

`gossr-assets-webpack-plugin` discovers entry points by convention: for each route directory it bundles
`index.ts`, `styles.scss` and — on reactive routes — the generated `__ssr_gen__.ts`. The webpack config ships
with `entry: {}`; the plugin fills it in. It also writes `webpack-assets.json`, which lists the output path,
the files per entry point and the image mapping.

## Emitting the tags

```html
<head>
    <ssr:assets/>
</head>
```

That expands to the `<link rel="stylesheet">` and `<script defer>` tags for the current route's chunk, with
content-hashed URLs. Ancestor routes contribute their own chunks to the same page, and each file is emitted
once no matter how many routes reference it. Put `<ssr:assets/>` in the root layout's `<head>`; a nested route
does not need its own.

## Images

An `<img src="…">` in a template is resolved through the image map:

```html
<img src="logo.png">
<!-- rendered as -->
<img src="/static/images/logo.b11feeeb5df711606111.png">
```

The path is relative to the route directory. Images referenced from SCSS are handled by webpack itself. Other
elements and attributes are not rewritten — a background image belongs in a stylesheet, and an unmanaged asset
can always be referenced by an absolute URL.

## Serving in development

Webpack writes the output directory (`static/` by default) and the generator embeds whatever it finds, so
`go run .` serves the assets from the binary in development too. `go-ssr -watch` re-runs webpack when a `.ts`
or `.scss` file changes and restarts the app.

## Production build

```bash
go-ssr -prod
go build -o myapp .
```

`-prod` runs webpack in production mode. For every emitted file the generator stages a copy under
`pages/static_embed/` — gzip-precompressed for compressible types such as CSS, JS, JSON and SVG, stored
verbatim for formats that are already compressed such as PNG, JPEG, WOFF2 and MP4 — and writes
`pages/ssrstaticfiles_gen.go` with an `embed.FS` and a URL-to-file map.

`NewSsrHandler` checks the static map first and falls through to the SSR router on a miss, so no
`http.FileServer` wiring is needed. `pkg/static` provides:

- a strong ETag from the stored bytes, with `If-None-Match` answered by 304,
- `Cache-Control: public, max-age=31536000, immutable`, which is safe because filenames are hashed,
- `Vary: Accept-Encoding`, gzip passed through when accepted and decompressed on the fly when not,
- `405` with an `Allow` header for anything other than GET and HEAD.

Staging is cached through an `.etags.json` manifest keyed by the source file's MD5 as well as its mtime, so
unchanged files are not re-compressed. Content hashing rather than mtime alone matters because `git checkout`,
`rsync -a`, `tar -p` and Docker `COPY` all preserve mtimes.

## Reserved paths and collisions

`pages/static_embed/` is build output, not a route. Add it to `.gitignore`:

```gitignore
**/pages/static_embed/
```

If an asset URL would collide with a route path — including one matched through a `_param_` segment —
generation fails rather than letting the static handler silently shadow the route. Change the webpack
`publicPath`/`outputPath` or rename the route.

## When assets look stale

The generator compares the manifest against the `.ts`, `.scss`, `.css` and image files under `pages/`, plus
`package.json`, the webpack config and `tsconfig.json`. If a source is newer than the last build, run webpack
again — plain `go-ssr` does this for you. Symptoms of a missed build are a page rendering without styles, or
`<ssr:assets/>` emitting nothing at all, which is what happens when `webpack-assets.json` is absent.
