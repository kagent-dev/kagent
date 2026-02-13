import Link from "next/link";

// SSO redirect path - defaults to oauth2-proxy's start endpoint
const SSO_REDIRECT_PATH = process.env.SSO_REDIRECT_PATH || "/oauth2/start";

// Kagent logo SVG - extracted from official logo
function KagentLogo({ size = 32 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="40 55 122 93"
      xmlns="http://www.w3.org/2000/svg"
    >
      {/* Main purple body */}
      <path
        fill="#922CE4"
        d="
        M126.997864,145.508286
        C109.839294,145.508240 93.177559,145.658081 76.523758,145.361786
        C73.724991,145.311996 70.298271,144.028458 68.295845,142.123077
        C59.993969,134.223557 52.105297,125.887978 44.132511,117.647102
        C43.281067,116.767021 42.355022,115.467758 42.337738,114.349434
        C42.194237,105.066231 42.256413,95.779854 42.256413,86.032257
        C47.341122,86.032257 51.926483,86.032257 56.961094,86.032257
        C56.961094,80.994217 56.961094,76.406288 56.961094,71.282379
        C61.991684,71.282379 66.574310,71.282379 71.670815,71.282379
        C71.670815,66.331810 71.670815,61.916893 71.670815,56.993835
        C79.472565,56.993835 86.758125,56.993828 94.043694,56.993835
        C104.872093,56.993847 115.704926,56.830112 126.525368,57.123623
        C128.860687,57.186970 131.684601,58.337463 133.372330,59.944229
        C141.681656,67.854942 149.714706,76.057953 157.754288,84.247116
        C158.705124,85.215652 159.652863,86.726158 159.665161,87.994507
        C159.800522,101.968422 159.750198,115.944138 159.750198,130.456604
        C154.855209,130.456604 150.279678,130.456604 145.223999,130.456604
        C145.223999,135.570984 145.223999,140.161850 145.223999,145.508316
        C139.010071,145.508316 133.253723,145.508316 126.997864,145.508286
      "
      />
      {/* White face/screen area */}
      <path
        fill="#FFFFFF"
        d="
        M86.003967,130.255005
        C81.190308,130.255020 76.875534,130.255020 72.153488,130.255020
        C72.153488,115.785019 72.153488,101.574821 72.153488,86.923309
        C96.069572,86.923309 120.065414,86.923309 144.409546,86.923309
        C144.409546,101.202568 144.409546,115.402832 144.409546,130.255005
        C125.002007,130.255005 105.752434,130.255005 86.003967,130.255005
      "
      />
      {/* Left eye */}
      <path
        fill="#922CE4"
        d="
        M99.702423,115.914185
        C95.077843,115.970016 90.926537,115.970016 86.393387,115.970016
        C86.393387,111.123573 86.393387,106.404976 86.393387,101.345261
        C90.982872,101.345261 95.696617,101.345261 101.281555,101.345261
        C100.909683,106.225632 100.542686,111.041985 99.702423,115.914185
      "
      />
      {/* Right eye */}
      <path
        fill="#922CE4"
        d="
        M115.489990,109.882820
        C115.489990,106.794655 115.489990,104.167953 115.489990,101.273529
        C120.500839,101.273529 125.050835,101.273529 129.931976,101.273529
        C129.931976,106.058121 129.931976,110.753593 129.931976,115.730949
        C125.249809,115.730949 120.673950,115.730949 115.489983,115.730949
        C115.489983,113.908028 115.489983,112.126152 115.489990,109.882820
      "
      />
    </svg>
  );
}

export default function LoginPage() {
  return (
    <>
      {/* Preload background image for faster rendering */}
      <link rel="preload" href="/login-bg.webp" as="image" type="image/webp" fetchPriority="high" />

      {/* CSS to hide header/footer and override main layout for fullscreen login */}
      <style>{`
        body:has(.login-page) > div header,
        body:has(.login-page) > div footer,
        body:has(.login-page) header:first-of-type,
        body:has(.login-page) footer {
          display: none !important;
        }
        body:has(.login-page) main {
          flex: unset !important;
          overflow: visible !important;
          height: 100vh !important;
          width: 100vw !important;
          max-width: unset !important;
          margin: 0 !important;
          padding: 0 !important;
        }
        body:has(.login-page) {
          overflow: hidden !important;
        }
      `}</style>

      <div className="login-page fixed inset-0 bg-[#0B0B15] text-white overflow-hidden z-50">
        {/* Background image with fade-in animation */}
        <div
          className="absolute inset-0 bg-cover bg-center bg-no-repeat animate-in fade-in duration-500 z-0"
          style={{ backgroundImage: "url('/login-bg.webp')" }}
        />

        {/* Header */}
        <header className="absolute top-0 left-0 w-full p-6 md:px-10">
          <div className="flex items-center gap-3">
            <KagentLogo size={32} />
            <span className="font-extrabold text-2xl tracking-tight text-white">kagent</span>
          </div>
        </header>

        {/* Main Content */}
        <main className="relative z-10 h-full flex flex-col justify-center items-center text-center p-5">
          <div className="bg-white/5 backdrop-blur-md border border-white/10 rounded-2xl px-6 py-8 md:px-12 md:py-10 max-w-[700px] flex flex-col items-center animate-in fade-in duration-500 delay-150 fill-mode-backwards">
            <h1 className="flex items-center gap-3 md:gap-5 font-extrabold text-4xl md:text-[6rem] leading-tight tracking-tighter mb-4 text-white [text-shadow:0_0_20px_rgba(168,85,247,0.6),0_0_60px_rgba(168,85,247,0.4)]">
              <KagentLogo size={80} />
              <span>kagent</span>
            </h1>
            <p className="text-lg md:text-2xl text-gray-300 max-w-[600px] font-normal mb-10 leading-relaxed">
              Orchestrating Your AI Future with Kubernetes and Beyond
            </p>

            <div className="relative z-10">
              <Link
                href={`${SSO_REDIRECT_PATH}?rd=/`}
                className="group relative inline-flex items-center justify-center gap-3 px-9 py-4 bg-gradient-to-r from-violet-500 to-fuchsia-500 rounded-full text-white font-semibold text-lg border-2 border-white/90 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-[0_10px_25px_-5px_rgba(139,92,246,0.5)]"
              >
                {/* Glow ring */}
                <span className="absolute -inset-2 rounded-full border-2 border-purple-500/40 shadow-[0_0_20px_rgba(168,85,247,0.3),inset_0_0_20px_rgba(168,85,247,0.2)] pointer-events-none transition-all duration-300 group-hover:border-purple-500/70 group-hover:shadow-[0_0_30px_rgba(168,85,247,0.6),inset_0_0_30px_rgba(168,85,247,0.3)]" />
                <KagentLogo size={24} />
                <span>Sign in with SSO</span>
              </Link>
            </div>
          </div>
        </main>
      </div>
    </>
  );
}
