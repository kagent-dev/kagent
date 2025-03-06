import KagentLogo from '@/components/kagent-logo'
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
          color: '#942DE7',
        }}
      >
        <KagentLogo />
      </div>
    ),
    {
      ...size,
    }
  )
}