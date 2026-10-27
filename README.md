# deckbuilder

A command-line Riftbound deckbuilder. Card data comes from the
[Riftcodex API](https://riftcodex.com).

```sh
go build -o deckbuilder .
./deckbuilder sync                                  # cache all cards locally
./deckbuilder search -type legend -tag jinx
./deckbuilder deck new jinx "Loose Cannon"
./deckbuilder deck champion jinx "Jinx - Rebel"
./deckbuilder deck add jinx "Blast Corps Cadet" 3
./deckbuilder deck add jinx "Fury Rune" 6           # runes/battlefields are routed automatically
./deckbuilder deck show jinx                        # decklist + rule check
./deckbuilder deck export jinx
```

Run `./deckbuilder help` for every command. Cards and decks are stored in
`$RIFTBOUND_HOME` (default `~/.riftbound`).

## Deck rules checked

- 1 legend; its domains define which cards are allowed (Colorless is always allowed)
- 1 chosen champion unit that shares the legend's champion tag
- Main deck of at least 40 cards (including the chosen champion)
- At most 3 copies of a card by name; alternate printings count together
- At most 3 signature cards, all matching the legend's champion
- Exactly 12 runes and 3 different battlefields; no tokens

## Layout

- `src.go` – CLI
- `internal/riftcodex` – API client
- `internal/cards` – local card cache, lookup and search
- `internal/deck` – deck model, storage and validation
