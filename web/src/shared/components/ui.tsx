import type { ButtonHTMLAttributes, ReactNode } from "react";
import { X } from "lucide-react";
import { humanizeStatus, statusTone } from "@/shared/lib/status";

export function Button({
  variant = "default",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "primary" | "ghost" | "danger";
}) {
  const variantClass = variant === "default" ? "" : variant;
  return <button className={`button ${variantClass} ${className}`} {...props} />;
}

export function Badge({
  tone = "default",
  children,
}: {
  tone?: "default" | "good" | "warn" | "bad";
  children: ReactNode;
}) {
  const cls = tone === "default" ? "" : tone;
  return <span className={`badge ${cls}`}>{children}</span>;
}

export function StatusBadge({ value }: { value: string }) {
  return <Badge tone={statusTone(value)}>{humanizeStatus(value)}</Badge>;
}

export function Panel({
  id,
  title,
  action,
  children,
}: {
  id?: string;
  title?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="panel" id={id}>
      {(title || action) && (
        <div className="panel-header">
          {title && <h2 className="panel-title">{title}</h2>}
          {action}
        </div>
      )}
      <div className="panel-body">{children}</div>
    </section>
  );
}

export function Metric({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="metric">
      <span className="metric-value">{value}</span>
      <span className="metric-label">{label}</span>
    </div>
  );
}

export function EmptyState({ title, detail }: { title: string; detail?: string }) {
  return (
    <div className="panel-body">
      <h3 className="panel-title">{title}</h3>
      {detail && <p className="page-subtitle">{detail}</p>}
    </div>
  );
}

export function Drawer({
  title,
  onClose,
  children,
}: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <aside className="drawer" role="dialog" aria-modal="true">
      <div className="drawer-header">
        <h2 className="panel-title">{title}</h2>
        <Button variant="ghost" onClick={onClose} aria-label="Close drawer">
          <X size={18} />
        </Button>
      </div>
      <div className="drawer-body">{children}</div>
    </aside>
  );
}

export function LoadingState({ label = "Loading" }: { label?: string }) {
  return <p className="muted">{label}...</p>;
}

export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span className="label">{label}</span>
      {children}
    </label>
  );
}
