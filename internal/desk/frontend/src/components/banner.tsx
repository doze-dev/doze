// The connection banner, in the dash's words ("daemon not responding ·
// retrying in 3s"); the page under it is faded, as the dash faded it.
export function Banner({ text }: { text: string }) {
  return (
    <div className="banner" role="status">
      {text}
    </div>
  );
}
