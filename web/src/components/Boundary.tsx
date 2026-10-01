import { Component, type ErrorInfo, type ReactNode } from "react";

import { Button } from "@app/components/Button";

/**
 * Keeps one broken piece from taking the page with it.
 *
 * A class, because catching a render is the one thing hooks cannot do. What it shows names
 * where the trouble is rather than what it was: the message React hands over is a stack trace's
 * first line, which tells somebody nothing they can act on. The console keeps the rest.
 *
 * Trying again only clears the flag, so anything that fails the same way twice says so again
 * rather than looping.
 */
export class Boundary extends Component<
  {
    children: ReactNode;
    what?: string;
    onReset?: () => void;
    /** The whole island: the screen, and a reload rather than a retry of the same tree. */
    page?: boolean;
  },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(
      `${this.props.what ?? "something"} failed to render`,
      error,
      info,
    );
  }

  render() {
    if (!this.state.failed) return this.props.children;
    if (this.props.page)
      return (
        <main className="flex min-h-dvh flex-col items-center justify-center gap-3 p-4 text-center">
          <p className="text-sm text-accent">
            Something broke and the page could not be shown.
          </p>
          <Button onClick={() => window.location.reload()}>Reload</Button>
        </main>
      );
    return (
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-sm text-accent">
          {this.props.what ?? "This"} could not be shown.
        </p>
        <Button
          onClick={() => {
            this.setState({ failed: false });
            this.props.onReset?.();
          }}
        >
          Try again
        </Button>
      </div>
    );
  }
}
