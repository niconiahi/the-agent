# partialjson

Reads the string fields of a JSON object that is still streaming. A tool call's arguments arrive as fragments of JSON, split anywhere: mid-key, mid-escape, mid-UTF-8 sequence. The edit preview in the follow window wants to show `path` and `old_text` as soon as they are complete and `new_text` as it grows, long before the arguments parse as JSON. It imports nothing and knows nothing about Neovim; `nvim/preview.go` is its one user.

`Read(text)` takes the concatenation of every fragment received so far and returns `Fields`: `Complete`, every top-level string field whose closing quote has arrived; `Streaming`, the key whose string value has opened but not closed (`""` when none is); and `Partial`, the decoded value of `Streaming` so far. Fields whose values are not strings are skipped over and left out. Reading stops quietly at whatever is still incomplete, so any prefix of valid JSON reads without error.

`Partial` never ends in half an escape, half a surrogate pair or half a UTF-8 sequence: the decoder stops before any escape or character still incomplete, so a prefix can always be shown as is and only grows as more fragments arrive. Escapes are decoded as JSON decodes them, `\uXXXX` surrogate pairs included.

`Read` re-reads the whole text each time. Tool arguments are small and the preview reads them at most once per flush, so keeping no state between calls is simpler than an incremental parser and costs nothing that shows.
