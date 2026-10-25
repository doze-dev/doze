# Brand

The doze family's icons: a sleeping bat on a tile in each product's colour.

| File | Perch | Bat | Tile |
|---|---|---|---|
| `doze.svg` | a branch, with z's | `#3A2448` | `#F0A24B` |
| `doze-kafka.svg` | the branch grown ring nodes | `#16150F` | `#FFD60A` |
| `doze-aws.svg` | a cloud marked AWS | `#1F2A44` | `#5BB8E8` |

`bat-source.svg` is the traced bat they're built from. `bun make.ts`
regenerates the three icons; `bun gallery.ts` writes `gallery.html`, which
shows them large and at 64, 32 and 16 px on light and dark.

The "AWS" letters are Poppins Bold (OFL) converted to outlines, so no font is
needed to render them.
