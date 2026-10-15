# Scrapscript

A exploration (read: very incomplete implementation) of [Scrapscript](https://scrapscript.org).

## Installation

With Go installed, run `go install github.com/Victorystick/scrapscript/cmd/scrap`.

## Overview

The aim is to implement Scrapscript along with the `scrap` CLI as documented on https://scrapscript.org/guide.

So far it supports

* `scrap eval` to evaluate a script passed over standard input.

* `scrap eval apply '...'` works like `scrap eval` but passes the result of the former to the function defined by `'...'`. For example:

    ```sh
    $ echo '0' \
        | scrap eval apply 'n -> n + 1' \
        | scrap eval apply 'n -> n + 1' \
        | scrap eval apply 'n -> n + 1'
    ```

* `scrap type` to infer the type of a script passed over standard input.

    ```sh
    $ echo 'list/fold 0 (total -> text -> total + text/length text)' | scrap type
    list text -> int
    ```

## Missing

* `scrap flat` - due to a lack of details.

* `scrap yard` - just TODO.

> Note: Scraps are currently fetched from the scrapyard at https://scraps.oseg.dev/ as text and cached locally.

## Differences from https://scrapscript.org

* Refers to scraps as `$source:<hex>`, the sha256 of their source, instead of `$sha1~~…`, as SHA-1 is cryptographically weak and `~~` otherwise means base64. During development, `$"./path"` refers to a draft file.

* No attempt to implement Scrap Maps, Scrap passes or Scrapbooks.
