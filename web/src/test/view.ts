/**
 * jsdom lays nothing out, so nothing is ever in view: an IntersectionObserver here is told of
 * nothing until a test says, with reach, that what is watched has come into view — as the end of
 * a list does when it is scrolled to.
 */
const watching = new Set<Watcher>();

export class Watcher {
  readonly root: Element | Document | null;
  readonly rootMargin: string;
  readonly thresholds = [0];
  private readonly nodes = new Set<Element>();

  constructor(
    private readonly told: IntersectionObserverCallback,
    init: IntersectionObserverInit = {},
  ) {
    this.root = init.root ?? null;
    this.rootMargin = init.rootMargin ?? "0px";
  }

  observe(node: Element) {
    this.nodes.add(node);
    watching.add(this);
  }

  unobserve(node: Element) {
    this.nodes.delete(node);
  }

  disconnect() {
    this.nodes.clear();
    watching.delete(this);
  }

  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }

  tell() {
    const entries = [...this.nodes].map(
      (target) =>
        ({
          target,
          isIntersecting: true,
          intersectionRatio: 1,
        }) as IntersectionObserverEntry,
    );
    if (entries.length > 0)
      this.told(entries, this as unknown as IntersectionObserver);
  }
}

/** Everything watched comes into view. */
export function reach() {
  for (const w of watching) w.tell();
}
