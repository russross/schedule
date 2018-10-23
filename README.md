Schedule optimizer
==================

TODO

About
-----

Installation
------------

`schedule.txt`
--------------

Using the CLI
-------------

Hints
-----

Web page
--------

Compile using:

    GOOS=js GOARCH=wasm go build -o schedule.wasm

Copy the following to a web server:

*   `index.html`
*   `schedule.json`
*   `schedule.txt`
*   `schedule.wasm`
*   `wasm_exec.js` (get this from `$(go env GOROOT)/misc/wasm/wasm_exec.js`)

Note that your web server must server `schedule.wasm` with MIME type
`application/wasm`. For apache, you may need to create a `.htaccess`
file in the directory with this line:

    AddType application/wasm .wasm

