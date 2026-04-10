import { Component, type ReactNode } from "react";

interface Props { children: ReactNode; }
interface State { hasError: boolean; error?: Error; }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="p-6 text-center rounded-lg border border-cp-red/30 bg-cp-red/5">
          <h2 className="text-lg font-semibold text-cp-red">Something went wrong</h2>
          <p className="text-sm text-muted-foreground mt-2 font-data">{this.state.error?.message}</p>
          <button
            onClick={() => window.location.reload()}
            className="mt-4 text-sm text-cp-purple underline"
          >
            Reload page
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
