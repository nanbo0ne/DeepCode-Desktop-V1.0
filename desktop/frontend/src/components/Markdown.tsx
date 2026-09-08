import { memo, useDeferredValue } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";
import { CodeViewer } from "./CodeViewer";
import { normalizeMath } from "./mathNormalize";
import { openExternal } from "../lib/bridge";

const components: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const match = /language-([\w-]+)/.exec(className ?? "");
    const isBlock = match !== null || text.includes("\n");
    if (isBlock) {
      return <CodeViewer value={text.replace(/\n$/, "")} language={match?.[1]} maxHeight={360} />;
    }
    return <code className="md-code">{children}</code>;
  },
  a: ({ href, children }) => (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        if (href) openExternal(href);
      }}
      onAuxClick={(e) => {
        e.preventDefault();
        if (href) openExternal(href);
      }}
      onMouseDown={(e) => {
        if (e.button === 1) e.preventDefault();
      }}
    >
      {children}
    </a>
  ),
};

export const Markdown = memo(function Markdown({
  text,
  showCursor,
}: {
  text: string;
  showCursor?: boolean;
}) {
  // Controller events are already frame-batched; do not delay each fragment again.
  const deferred = useDeferredValue(text);

  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={components}
      >
        {normalizeMath(deferred)}
      </ReactMarkdown>
      {showCursor && <span className="cursor" data-streaming-cursor="true" />}
    </div>
  );
});
