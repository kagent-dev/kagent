import { ImageResponse } from 'next/og'

export const size = {
  width: 378,
  height: 286,
}
export const contentType = 'image/png'

export default function Icon() {
  return new ImageResponse(
    (
      <div
        style={{
          fontSize: 24,
          width: '100%',
          height: '100%',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#151927',
          backgroundColor: 'white',
          fontWeight: 700,
          letterSpacing: '-0.02em',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '12px',
          }}
        >
          {/* Noble Wolf SVG */}
          <svg width="32" height="32" viewBox="0 0 80 80" fill="none" xmlns="http://www.w3.org/2000/svg">
            {/* Noble Crest */}
            <path d="M40 8 C36 12, 36 16, 40 20 C44 16, 44 12, 40 8 Z" fill="#151927" opacity="0.8"/>
            <path d="M38 10 L42 10 L41 14 L39 14 Z" fill="#151927" opacity="0.6"/>

            {/* Wolf Face */}
            <path
              d="M12 30c0-4.2 2.1-8.1 5.5-10.5l12-8.7c4.2-3 9.5-3 13.7 0L55.2 19.5c3.4 2.4 5.5 6.3 5.5 10.5v9.7c0 7.2-3.9 13.8-10.2 17.7l-9.4 5.8a5.1 5.1 0 0 1-5.1 0l-9.4-5.8C16.9 53.5 13 46.9 13 39.7V30Z"
              stroke="#151927"
              strokeWidth="2.5"
              strokeLinejoin="round"
              fill="#151927"
              fillOpacity="0.1"
            />

            {/* Enhanced Ears */}
            <path d="M20 22 L28 10 L30 23" stroke="#151927" strokeWidth="2.5" strokeLinecap="round" />
            <path d="M60 22 L52 10 L50 23" stroke="#151927" strokeWidth="2.5" strokeLinecap="round" />

            {/* Noble Eyes */}
            <path d="M25 32 q3.5 -2.5 7 0" stroke="#151927" strokeWidth="2.5" strokeLinecap="round" />
            <path d="M48 32 q3.5 -2.5 7 0" stroke="#151927" strokeWidth="2.5" strokeLinecap="round" />
            <circle cx="28.5" cy="34" r="2" fill="#151927" />
            <circle cx="51.5" cy="34" r="2" fill="#151927" />

            {/* Distinguished Muzzle */}
            <path d="M30 40 C35 42.5, 45 42.5, 50 40" stroke="#151927" strokeWidth="2.5" strokeLinecap="round" />
            <circle cx="40" cy="44" r="2" fill="#151927" />

            {/* Strong Jawline */}
            <path d="M25 47 C32 52, 48 52, 55 47" stroke="#151927" strokeWidth="2.5" strokeLinecap="round" />
          </svg>

          <span>adolphe.ai</span>
        </div>
      </div>
    ),
    {
      ...size,
    }
  )
}