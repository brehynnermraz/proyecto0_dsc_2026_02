export default function AuthIllustration({ caption }: { caption: string }) {
  return (
    <div className="relative h-full w-full overflow-hidden rounded-3xl">
      <svg
        viewBox="0 0 600 800"
        preserveAspectRatio="xMidYMid slice"
        className="absolute inset-0 h-full w-full"
        role="img"
        aria-hidden="true"
      >
        <defs>
          <linearGradient id="sky" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#fcd9b6" />
            <stop offset="45%" stopColor="#e59aa8" />
            <stop offset="75%" stopColor="#a97fb0" />
            <stop offset="100%" stopColor="#5b4b8a" />
          </linearGradient>
          <radialGradient id="sun" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#fff6e0" />
            <stop offset="60%" stopColor="#ffe0a3" />
            <stop offset="100%" stopColor="#ffcf80" stopOpacity="0" />
          </radialGradient>
          <linearGradient id="hillFar" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#8b6fa8" />
            <stop offset="100%" stopColor="#6b5590" />
          </linearGradient>
          <linearGradient id="hillMid" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#5f4c82" />
            <stop offset="100%" stopColor="#493a68" />
          </linearGradient>
        </defs>

        <rect width="600" height="800" fill="url(#sky)" />

        <circle cx="230" cy="230" r="150" fill="url(#sun)" />
        <circle cx="230" cy="230" r="70" fill="#fff2d6" />

        <path
          d="M0 380 C 90 320, 180 340, 260 300 C 340 260, 430 300, 600 260 L600 500 L0 500 Z"
          fill="url(#hillFar)"
          opacity="0.9"
        />
        <path
          d="M0 470 C 100 400, 220 430, 320 390 C 420 350, 500 400, 600 370 L600 560 L0 560 Z"
          fill="url(#hillMid)"
        />
        <path
          d="M0 560 C 120 500, 260 540, 380 500 C 460 475, 520 505, 600 480 L600 800 L0 800 Z"
          fill="#2d2447"
        />

        {[
          [70, 560, 60],
          [130, 590, 45],
          [500, 540, 55],
          [550, 580, 40],
        ].map(([x, y, h], i) => (
          <g key={i} transform={`translate(${x} ${y})`} opacity="0.85">
            <rect x="-2" y="0" width="4" height={h} fill="#1f1a35" />
            <circle cx="0" cy="-6" r="16" fill="#1f1a35" />
            <circle cx="-12" cy="6" r="12" fill="#1f1a35" />
            <circle cx="12" cy="10" r="12" fill="#1f1a35" />
          </g>
        ))}

        <ellipse cx="300" cy="640" rx="280" ry="18" fill="#1a1530" opacity="0.6" />
      </svg>

      <div className="absolute inset-x-0 bottom-0 p-8 text-white">
        <p className="text-lg font-medium drop-shadow-sm">{caption}</p>
      </div>
    </div>
  );
}
