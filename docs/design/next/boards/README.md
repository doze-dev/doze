# Boards

The dash is built from these. Each file is one artboard of the canvas at
https://claude.ai/artifact/2wonaARTbNUxwRM6K26Sko, saved as published. `term.css` is
the palette and the type; `Main.dc.html` is Home, `Focus.dc.html` a database module,
`Narrow.dc.html` 80 x 24.

A screen is done when a frame drawn by the real program has been put beside its board
and the person who drew the board agrees. Goldens are checked in only after that.

## The look tool

```
cd tools/look
go run . palette crash         # some scenes
go run .                       # all of them
```

It runs the real dash in a pseudo-terminal against the shop fixture
(`internal/dash/fixture`), presses the scene's real keys, reads the screen back
through a terminal emulator, and writes `.look/<scene>.png`: the board on the left,
the dash on the right, both drawn by the same Chrome at the board's size. Read that
image. `go run . live -home /tmp/dz-try -dir ./project` does the same against a real
daemon, with no board.

It cannot judge your terminal's fonts, how it draws odd glyphs, or colour on your
display.
