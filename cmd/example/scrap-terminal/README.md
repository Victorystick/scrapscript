# scrap-terminal

A host of the terminal platform: it runs a scrapscript program over the lines
of standard input and prints what the program outputs. It is an example of a
host built from bindings that `scrap-gen-go` generates from a platform's
interface (SPEC.md section 13).

## Usage

```sh
$ go run ./cmd/example/scrap-terminal --help
scrap-terminal is a host which embeds $source:849e426b… with the type
…
$ printf 'a\nb\n' | go run ./cmd/example/scrap-terminal echo.scrap
a
b
```

The script is a path to a draft, or a reference `source:<hex>`. The host exits
with the status the program asks for, 1 if the script fails to load or run,
and 2 on bad usage.

## Writing a program

A program is a scrap whose value has the type of the interface,
[program.scrap](program.scrap):

```scrap
; program     : type -> type = s => { init : turn s, step : event -> s -> turn s }
; turn        : type -> type = s => { state : s, out : text, next : next-action }
; event       : type = [#line text, #eof]
; next-action : type = [#read-line, #exit int]
```

The program keeps its own state, of a type `s` that it chooses and the host
never inspects. The host prints `init.out`, then, while the program asks to
`#read-line`, passes `step` the next line, or `#eof` once the input ends,
along with the state, and prints the `out` of each turn, until the program
asks to `#exit`.

`step` takes the event and then the state, so its arms can match on both,
with one pattern per argument; the first arm whose patterns all match wins.
This program exits after reading two lines, or at the end of the input:

```scrap
{ init = { state = 0, out = "", next = #read-line }
, step = | #line _ -> 1 -> { state = 2, out = "two\n", next = #exit 0 }
         | #line _ -> n -> { state = n + 1, out = "", next = #read-line }
         | #eof    -> n -> { state = n, out = "", next = #exit 1 }
}
```

A program can refer to the interface by its hash, which `--help` prints:

```scrap
echo
; echo : $source:849e426be10d4955c969e7ece9acc1e21072e073965970a2fd0519bf076a683f () =
  { init = { state = (), out = "", next = #read-line }
  , step = | #line text -> state -> { state = state, out = text ++ "\n", next = #read-line }
           | #eof       -> state -> { state = state, out = "",           next = #exit 0 }
  }
```

The host embeds the interface, so this works without the network, even though
the interface isn't published.

## How it works

| File | |
|---|---|
| `program.scrap` | The interface. |
| `bindings.go` | Generated: the interface embedded by its hash, Go types for its types, conversions between them and values, and `Load`. |
| `main.go` | Generated: runs the command line shared by all hosts (the `host` package), then `run`. |
| `run.go` | Written by hand: `run` and `Run`, which carry out the platform's effects. |
| `run_test.go` | Checks that `Run` agrees with the platform's reference semantics. |

1. `cli.Main`, in `host/cli`, parses the command line and reads the script. Scraps that it
   refers to by hash are looked up among the embedded scraps first, and then
   in the scrapyard.
2. `Load` checks the script against the interface, once, then evaluates it and
   converts its value to a `Program`. A script that doesn't fit the interface,
   such as one whose `step` has no arm for `#eof`, fails here.
3. `run` calls `Run`, which feeds lines to `Program.Step` and writes each
   `Turn.Out`. The state passes through as an opaque `S`, and calls into the
   script are no longer type-checked.

`run_test.go` runs several programs on several inputs both through `Run` and
through `simulate.scrap`, the platform's reference semantics written in
scrapscript, and requires the same output and status.

## Regenerating

After changing `program.scrap`, regenerate the bindings:

```sh
go generate ./cmd/example/scrap-terminal
```

The interface's hash changes with its source, so scripts that refer to it by
hash must be updated too. The `gogen` tests fail if the generated files are out
of date.
