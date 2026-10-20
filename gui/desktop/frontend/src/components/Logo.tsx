export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path
        d="M32 3.5 56 13v18.2C56 46.6 45.8 56.7 32 60.5 18.2 56.7 8 46.6 8 31.2V13Z"
        fill="#0e1420"
        stroke="#17c7bd"
        strokeWidth="3.5"
        strokeLinejoin="round"
      />
      <path
        d="m22 32.5 7 7 14-15"
        stroke="#17c7bd"
        strokeWidth="5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
