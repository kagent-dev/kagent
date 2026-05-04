import Link from "next/link";
import KagentLogo from "@/components/kagent-logo";
import { skipToContentLinkClassName } from "@/lib/skipToContent";
import { cn } from "@/lib/utils";

// SSO redirect path - defaults to oauth2-proxy's start endpoint
const SSO_REDIRECT_PATH = process.env.SSO_REDIRECT_PATH || "/oauth2/start";

export default function LoginPage() {
  return (
    <>
      {/* Preload background image for faster rendering */}
      <link rel="preload" href="/login-bg.webp" as="image" type="image/webp" fetchPriority="high" />

      <div className="login-page relative fixed inset-0 z-50 overflow-hidden bg-[#0B0B15] text-white">
        <a
          href="#login-main"
          className={cn(skipToContentLinkClassName, "text-white/90")}
        >
          Skip to content
        </a>
        {/* Background image with fade-in animation */}
        <div
          className="absolute inset-0 bg-cover bg-center bg-no-repeat animate-in fade-in duration-500 z-0"
          style={{ backgroundImage: "url('/login-bg.webp')" }}
        />

        {/* Header */}
        <header className="absolute top-0 left-0 w-full p-6 md:px-10">
          <div className="flex items-center gap-3">
            <KagentLogo className="w-8 h-8" />
            <span className="font-extrabold text-2xl tracking-tight text-white">kagent</span>
          </div>
        </header>

        <main
          id="login-main"
          className="relative z-10 flex h-full flex-col items-center justify-center p-5 text-center outline-none focus:outline-none scroll-mt-8"
          tabIndex={-1}
          aria-labelledby="login-title"
        >
          <div className="max-w-[700px] flex flex-col items-center rounded-2xl border border-white/10 bg-white/5 px-6 py-8 shadow-sm backdrop-blur-md animate-in fade-in duration-500 delay-150 fill-mode-backwards md:px-12 md:py-10">
            <h1
              id="login-title"
              className="mb-4 flex items-center gap-3 text-4xl font-extrabold leading-tight tracking-tighter text-white md:gap-5 md:text-[6rem] [text-shadow:0_0_20px_rgba(168,85,247,0.6),0_0_60px_rgba(168,85,247,0.4)]"
            >
              <KagentLogo className="h-20 w-20" />
              <span className="text-balance">kagent</span>
            </h1>
            <p className="text-lg md:text-2xl text-gray-300 max-w-[600px] font-normal mb-10 leading-relaxed">
              Bringing Agentic AI to cloud native
            </p>

            <div className="relative z-10">
              <Link
                href={`${SSO_REDIRECT_PATH}?rd=/`}
                className="group relative inline-flex items-center justify-center gap-3 px-9 py-4 bg-gradient-to-r from-violet-500 to-fuchsia-500 rounded-full text-white font-semibold text-lg border-2 border-white/90 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-[0_10px_25px_-5px_rgba(139,92,246,0.5)]"
              >
                {/* Glow ring */}
                <span className="absolute -inset-2 rounded-full border-2 border-purple-500/40 shadow-[0_0_20px_rgba(168,85,247,0.3),inset_0_0_20px_rgba(168,85,247,0.2)] pointer-events-none transition-all duration-300 group-hover:border-purple-500/70 group-hover:shadow-[0_0_30px_rgba(168,85,247,0.6),inset_0_0_30px_rgba(168,85,247,0.3)]" />
                <KagentLogo className="w-6 h-6" />
                <span>Sign in with SSO</span>
              </Link>
            </div>
          </div>
        </main>
      </div>
    </>
  );
}
