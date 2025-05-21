# Scrapscript

A exploration (read: very incomplete implementation) of [Scrapscript](https://scrapscript.org).

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

## Known bugs

* Incomplete scanner
  * the `bytes/to-utf8-text` function is available via  `bytes-to-utf8-text`

* Only supports pattern matching on the argument immediately following a pipe.

    ```sh
    f 1 3
    ; f =
    | 1 -> 1 -> 1
    | 1 -> 2 -> 3
    | 3 -> 5 -> 8
    | 5 -> 8 -> 12
    | a -> b -> a + b

    # evaluation error: function parameter must be an identifier
    #     3: | 1 -> 1 -> 1
    #               ^
    ```

## Missing

* `scrap flat` - due to a lack of details.

* `scrap yard` - just TODO.

> Note: Scraps are currently fetched from the scrapyard at https://scraps.oseg.dev/ as text and cached locally.

## Differences from https://scrapscript.org

* Defines a `$sha256` function to import scraps instead of `$sha1` as the latter is cryptographically weak.

* No attempt to implement Scrap Maps, Scrap passes or Scrapbooks.
