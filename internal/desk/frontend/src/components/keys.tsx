// The keys bar along the bottom (the boards' .keys): what the keyboard does
// on this page, only what works, with the board's note at the right.
export function Keys({ keys, note }: { keys: Array<[string, string]>; note?: string }) {
  return (
    <div className="keys">
      {keys.map(([k, what]) => (
        <span key={k}>
          <kbd>{k}</kbd>
          {what}
        </span>
      ))}
      <span className="grow" />
      {note && <span className="note">{note}</span>}
    </div>
  );
}
