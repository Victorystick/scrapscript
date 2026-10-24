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

* `scrap test` to run the test cases of a script passed over standard input, made with `lib/test.scrap` and grouped in records and lists. It prints each failing case and exits with status 1 if any failed.

    ```sh
    $ cd lib && printf '{ adds = t.eq (1 + 1) 3 }\n; t = $"./test.scrap"' | scrap test
    FAIL adds
        got  2
        want 3
    1 failed, 0 passed
    ```

* `scrap push` to publish a script passed over standard input to the scrapyard, printing its hash. The drafts it refers to by path, such as `$"./test.scrap"`, are published first, each with its paths replaced by `$source:<hex>` of the published draft.

    ```sh
    $ cd lib && echo '$"./test.scrap"' | scrap push
    ```

* `--root dir` reads the script as if it were a file in `dir`, which relative imports such as `$"./test.scrap"` resolve from, and `--cache dir` keeps fetched scraps in `dir` instead of the user cache directory. Flags go before or after the command:

    ```sh
    $ scrap eval --root ./lib --cache ./tmp-scraps < some.scrap
    ```

## Missing

* `scrap flat` - due to a lack of details.

* `scrap yard` - just TODO.

> Note: Scraps are currently fetched from the scrapyard at https://scraps.oseg.dev/ as text and cached locally.

## Differences from https://scrapscript.org

* Refers to scraps as `$source:<hex>`, the sha256 of their source, instead of `$sha1~~…`, as SHA-1 is cryptographically weak and `~~` otherwise means base64. During development, `$"./path"` refers to a draft file.

* No attempt to implement Scrap Maps, Scrap passes or Scrapbooks.
