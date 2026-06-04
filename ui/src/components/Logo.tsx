export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="32" height="32" rx="7" fill="#c9a227" />
      <path
        d="M10 10h5v5h-5zM17 10h5v5h-5zM10 17h5v5h-5zM17 17h5v5h-5z"
        fill="#1a1400"
        opacity="0.9"
      />
      <path d="M14 12.5h4M12.5 14v4M19.5 14v4M14 19.5h4" stroke="#1a1400" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  );
}

export function LogoWithText({ size = 28 }: { size?: number }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
      <Logo size={size} />
      <span style={{ fontSize: 17, fontWeight: 700, color: "var(--text-heading)", letterSpacing: "-0.02em" }}>
        Hive
      </span>
    </div>
  );
}
