// The app's shell. Pages, tabs and views arrive with P1; this is the shape
// they mount into, and it is what the first build and the first budget are
// measured on.
export function App() {
  return (
    <main className="shell">
      <header className="tabs" role="tablist" aria-label="Stacks" />
      <section className="page">
        <p className="muted">doze</p>
      </section>
    </main>
  );
}
