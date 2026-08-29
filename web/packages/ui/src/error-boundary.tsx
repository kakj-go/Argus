import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "./button";
import { useUiText } from "./locale";

type FallbackProps = {
  error: unknown;
  onReset: () => void;
};

/**
 * Last-resort render guard: a single crashing subtree must degrade to a
 * recoverable fallback instead of unmounting the whole application tree.
 * Place it once at the app shell; finer-grained boundaries can nest inside.
 */
class ErrorBoundary extends Component<
  { children: ReactNode; fallback?: (props: FallbackProps) => ReactNode; onReset?: () => void },
  { error: unknown }
> {
  state = { error: undefined as unknown };

  static getDerivedStateFromError(error: unknown) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("[ErrorBoundary] Uncaught render error:", error, info.componentStack);
  }

  reset = () => {
    this.setState({ error: undefined });
    this.props.onReset?.();
  };

  render() {
    if (this.state.error !== undefined) {
      if (this.props.fallback) return this.props.fallback({ error: this.state.error, onReset: this.reset });
      return <DefaultFallback error={this.state.error} onReset={this.reset} />;
    }
    return this.props.children;
  }
}

function DefaultFallback({ error, onReset }: FallbackProps) {
  const text = useUiText();
  return (
    <main className="argus-error-boundary" role="alert">
      <h1>{text("界面出现异常", "Something went wrong")}</h1>
      <p>{text("页面渲染遇到未处理的错误，你可以重试或刷新页面。", "The page hit an unhandled error while rendering. Retry or reload.")}</p>
      {error instanceof Error && <pre>{error.message}</pre>}
      <div className="argus-error-boundary__actions">
        <Button onClick={onReset} variant="primary">
          {text("重试", "Retry")}
        </Button>
        <Button onClick={() => window.location.reload()} variant="secondary">
          {text("刷新页面", "Reload page")}
        </Button>
      </div>
    </main>
  );
}

export default ErrorBoundary;
