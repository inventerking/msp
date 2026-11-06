import DOMPurify from "dompurify";

const allowedTags = ["p", "br", "strong", "em", "ul", "ol", "li", "a", "code"];
const allowedAttributes = ["href"];

/** The single place user or AI-authored rich text may be turned into HTML. */
export function sanitizeRichText(html: string): string {
  return DOMPurify.sanitize(html, { ALLOWED_TAGS: allowedTags, ALLOWED_ATTR: allowedAttributes, ALLOWED_URI_REGEXP: /^(?:https?:|mailto:)/i });
}
