'use client'

import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Download, ShieldCheck, Shield, Zap, Globe2, Split, Layers, Bug, Eye, Server, Loader2, Check, Copy, UploadCloud, XCircle, AlertTriangle, MousePointerClick } from 'lucide-react'

type Platform = {
  id: string
  name: string
  file: string
  size: string
  desc: string
  badge?: string
  note: string
  icon: 'windows' | 'macos' | 'android'
}

const PLATFORMS: Platform[] = [
  {
    id: 'android',
    name: 'Android',
    file: 'AegisVPN-android.apk',
    size: '6.7 MB',
    desc: 'Android 7.0+ · arm64, armv7, x86, x86_64 · signed (v2+v3)',
    badge: 'APK',
    note: 'Real WireGuard Go engine (lib/<abi>/libwg-go.so). Enable "Install unknown apps" for your browser. If an older build was installed, uninstall it first.',
    icon: 'android',
  },
  {
    id: 'windows',
    name: 'Windows',
    file: 'AegisVPN-Setup-x64.exe',
    size: '6.9 MB',
    desc: 'Windows 10/11 x64 · installer with shortcuts + uninstaller',
    badge: 'EXE',
    note: 'SmartScreen: "More info" → "Run anyway" (unsigned build). Installs the branded GUI, the aegisvpn.exe engine, the 80-node registry and service scripts.',
    icon: 'windows',
  },
  {
    id: 'macos',
    name: 'macOS',
    file: 'AegisVPN-1.2.1.dmg',
    size: '14.8 MB',
    desc: 'v1.2.1 · macOS 12+ · Apple Silicon & Intel · signed universal app',
    badge: 'DMG',
    note: 'v1.2.1 has a fresh filename — you cannot get a cached copy of the earlier broken build. The app is ~15 MB by design (native Go engine — see “Why only ~15 MB?” below). One-line Terminal install recommended: zero Gatekeeper dialogs.',
    icon: 'macos',
  },
]

const FEATURES = [
  { icon: Shield, title: 'Kill Switch', desc: 'Firewall lockdown keeps traffic inside the tunnel — zero leaks on drop.' },
  { icon: Layers, title: 'Double VPN', desc: 'Multi-hop chains route traffic through two separate nodes.' },
  { icon: Bug, title: 'Threat Protection', desc: 'DNS-layer blocking of ads, trackers and malware domains.' },
  { icon: Split, title: 'Split Tunneling', desc: 'Choose per-app which traffic goes through the tunnel.' },
  { icon: Eye, title: 'Dark Web Monitor', desc: 'Credential breach scanning for your addresses.' },
  { icon: Zap, title: 'NordLynx-class speed', desc: 'wireguard-go core: Curve25519 + ChaCha20-Poly1305.' },
]

function Icon({ kind }: { kind: Platform['icon'] }) {
  if (kind === 'android')
    return (
      <svg viewBox="0 0 24 24" className="h-7 w-7" fill="currentColor" aria-hidden>
        <path d="M17.6 9.48l1.84-3.18c.16-.31.04-.7-.26-.85-.29-.15-.65-.06-.83.22l-1.88 3.24a11.43 11.43 0 0 0-8.94 0L5.65 5.67c-.19-.29-.58-.38-.87-.2-.28.18-.37.54-.22.83L6.4 9.48A10.78 10.78 0 0 0 1 18h22a10.78 10.78 0 0 0-5.4-8.52zM7 15.25a1.25 1.25 0 1 1 0-2.5 1.25 1.25 0 0 1 0 2.5zm10 0a1.25 1.25 0 1 1 0-2.5 1.25 1.25 0 0 1 0 2.5z" />
      </svg>
    )
  if (kind === 'windows')
    return (
      <svg viewBox="0 0 24 24" className="h-7 w-7" fill="currentColor" aria-hidden>
        <path d="M3 5.55l7.03-.97v6.81H3V5.55zm0 12.9l7.03.97v-6.73H3v5.76zm7.79 1.09L21 21.5V12.7h-10.2v6.84zM10.79 4.4v7.06H21V2.5l-10.21 1.9z" />
      </svg>
    )
  return (
    <svg viewBox="0 0 24 24" className="h-7 w-7" fill="currentColor" aria-hidden>
      <path d="M16.365 12.79c-.03-2.63 2.15-3.89 2.25-3.95-1.23-1.79-3.14-2.04-3.81-2.07-1.62-.16-3.16.95-3.98.95-.82 0-2.09-.93-3.43-.9-1.77.03-3.4 1.03-4.31 2.61-1.84 3.19-.47 7.92 1.32 10.51.88 1.27 1.93 2.69 3.3 2.64 1.33-.05 1.83-.85 3.43-.85s2.05.85 3.45.82c1.43-.02 2.33-1.29 3.2-2.57 1-1.46 1.41-2.88 1.44-2.95-.03-.02-2.76-1.06-2.86-4.24zM13.7 4.98c.72-.88 1.21-2.1 1.08-3.31-1.04.04-2.31.69-3.06 1.57-.67.78-1.26 2.03-1.1 3.23 1.16.09 2.36-.59 3.08-1.49z" />
    </svg>
  )
}

function ChecksumRow({ file, hash }: { file: string; hash: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-2 py-1.5 text-xs">
      <span className="w-52 shrink-0 font-mono text-zinc-300">{file}</span>
      <span className="truncate font-mono text-zinc-500">{hash}</span>
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7 shrink-0 text-zinc-400 hover:text-white"
        aria-label={`Copy checksum for ${file}`}
        onClick={() => {
          navigator.clipboard.writeText(hash)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        }}
      >
        {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
      </Button>
    </div>
  )
}

function CopyCommand({ cmd }: { cmd: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="mt-2 flex items-center gap-2 rounded-xl border border-emerald-500/25 bg-black/50 py-2 pl-3 pr-2">
      <code className="min-w-0 flex-1 truncate font-mono text-xs text-emerald-300">{cmd}</code>
      <Button
        variant="ghost"
        size="sm"
        className="h-7 shrink-0 gap-1.5 px-2 text-xs text-zinc-300 hover:bg-white/10 hover:text-white"
        aria-label={`Copy terminal command: ${cmd}`}
        onClick={() => {
          navigator.clipboard.writeText(cmd)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        }}
      >
        {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
        {copied ? 'Copied' : 'Copy'}
      </Button>
    </div>
  )
}

type SecFix = {
  os: 'windows' | 'macos' | 'android'
  label: string
  dialog: string
  steps: string[]
}

const SEC_FIXES: SecFix[] = [
  {
    os: 'windows',
    label: 'Windows',
    dialog: '“Windows protected your PC” (SmartScreen)',
    steps: [
      'Run the downloaded AegisVPN-Setup-x64.exe.',
      'On the blue SmartScreen click “More info”, then “Run anyway”. This appears for every app without a paid EV certificate — it does not mean the file is broken.',
      'UAC prompt “Do you want to allow this app to make changes?” → Yes.',
      'Install, then open AegisVPN from the Start menu. Antivirus flagged it? Choose Allow / restore it from quarantine.',
    ],
  },
  {
    os: 'macos',
    label: 'macOS',
    dialog: '“AegisVPN Not Opened — Apple could not verify”',
    steps: [
      'Best: use the one-line installer below — it downloads without the quarantine stamp, so Gatekeeper never appears.',
      'DMG route: open AegisVPN-1.2.1.dmg, drag the app to Applications, click Done on the dialog.',
      'System Settings → Privacy & Security → “Open Anyway” next to AegisVPN → Open — or paste the xattr command below in Terminal.',
      'Approve the admin password once (the tunnel needs utun) — the dashboard opens in your browser.',
    ],
  },
  {
    os: 'android',
    label: 'Android',
    dialog: '“Unsafe app blocked” (Play Protect)',
    steps: [
      'Tap the downloaded APK → when Android asks, allow your browser to “install unknown apps”.',
      'If Play Protect says “Unsafe app blocked” → tap “More details” → “Install anyway”. Expected for self-signed APKs outside the Play Store.',
      'Open AegisVPN → VPN “Connection request” → OK.',
      '“App not installed”? An older build is present — uninstall it first, then install again.',
    ],
  },
]

function DropZone() {
  const [st, setSt] = useState<{ status: 'idle' | 'busy' | 'ok' | 'bad' | 'unknown'; msg: string }>({ status: 'idle', msg: '' })
  const [over, setOver] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  async function check(file: File) {
    setSt({ status: 'busy', msg: `Computing SHA-256 of ${file.name}…` })
    try {
      const buf = await file.arrayBuffer()
      const digest = await crypto.subtle.digest('SHA-256', buf)
      const hex = Array.from(new Uint8Array(digest)).map((b) => b.toString(16).padStart(2, '0')).join('')
      const sums = await fetch('/downloads/SHA256SUMS.txt').then((r) => r.text())
      const row = sums
        .trim()
        .split('\n')
        .map((l) => l.split(/\s+/))
        .find((parts) => (parts[1] || '').replace(/^\*/, '') === file.name)
      if (!row) {
        setSt({ status: 'unknown', msg: `“${file.name}” is not one of this site's release files. Only download installers from this page.` })
      } else if (row[0] === hex) {
        setSt({ status: 'ok', msg: `${file.name} is byte-perfect — SHA-256 verified. The file is good. If it still won't open, it is your device's security screen: follow the exact clicks for your OS above.` })
      } else {
        setSt({ status: 'bad', msg: `${file.name} does NOT match this site's build (got ${hex.slice(0, 12)}…). You received an old cached or corrupted copy — delete it and re-download from this page.` })
      }
    } catch {
      setSt({ status: 'bad', msg: 'Could not read that file — try again.' })
    }
  }

  const tone =
    st.status === 'ok'
      ? 'border-emerald-500/30 bg-emerald-500/[0.07] text-emerald-200'
      : st.status === 'bad'
        ? 'border-red-500/30 bg-red-500/[0.07] text-red-200'
        : 'border-amber-500/30 bg-amber-500/[0.07] text-amber-200'

  return (
    <div>
      <div
        role="button"
        tabIndex={0}
        aria-label="Drop a downloaded file here to verify its SHA-256 checksum"
        onDragOver={(e) => {
          e.preventDefault()
          setOver(true)
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setOver(false)
          const f = e.dataTransfer.files?.[0]
          if (f) check(f)
        }}
        onClick={() => inputRef.current?.click()}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            inputRef.current?.click()
          }
        }}
        className={`flex cursor-pointer flex-col items-center justify-center gap-1.5 rounded-xl border-2 border-dashed p-5 text-center transition-colors ${over ? 'border-emerald-400 bg-emerald-500/10' : 'border-white/15 bg-black/30 hover:border-violet-500/40'}`}
      >
        <input
          ref={inputRef}
          type="file"
          className="hidden"
          aria-hidden
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) check(f)
            e.currentTarget.value = ''
          }}
        />
        {st.status === 'busy' ? (
          <Loader2 className="h-6 w-6 animate-spin text-violet-300" aria-hidden />
        ) : (
          <UploadCloud className="h-6 w-6 text-zinc-400" aria-hidden />
        )}
        <p className="text-sm text-zinc-300">Drop the downloaded file here — or tap to choose it</p>
        <p className="text-xs text-zinc-500">SHA-256 is computed locally in your browser. Nothing is uploaded.</p>
      </div>
      {st.status !== 'idle' && (
        <div className={`mt-3 flex items-start gap-2 rounded-xl border p-3 text-sm leading-relaxed ${tone}`} role="status">
          {st.status === 'busy' && <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin" aria-hidden />}
          {st.status === 'ok' && <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-emerald-400" aria-hidden />}
          {st.status === 'bad' && <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-red-400" aria-hidden />}
          {st.status === 'unknown' && <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" aria-hidden />}
          <span>{st.msg}</span>
        </div>
      )}
    </div>
  )
}

export default function Home() {
  const [hashes, setHashes] = useState<[string, string][] | null>(null)
  const [loadingHashes, setLoadingHashes] = useState(false)
  const [origin, setOrigin] = useState('')
  const [userOS, setUserOS] = useState<'windows' | 'macos' | 'android' | null>(null)

  useEffect(() => {
    setOrigin(window.location.origin)
    const ua = navigator.userAgent
    if (/Android/i.test(ua)) setUserOS('android')
    else if (/Windows/i.test(ua)) setUserOS('windows')
    else if (/Macintosh|Mac OS X/i.test(ua)) setUserOS('macos')
  }, [])

  async function loadHashes() {
    setLoadingHashes(true)
    try {
      const res = await fetch('/downloads/SHA256SUMS.txt')
      const text = await res.text()
      const parsed: [string, string][] = text
        .trim()
        .split('\n')
        .map((line) => {
          const [h, f] = line.split(/\s+/)
          return [f.replace(/^\*/, ''), h] as [string, string]
        })
      setHashes(parsed)
    } catch {
      setHashes(null)
    } finally {
      setLoadingHashes(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#030307] text-zinc-100 flex flex-col">
      {/* ambient glow */}
      <div
        aria-hidden
        className="pointer-events-none fixed inset-0"
        style={{
          background:
            'radial-gradient(600px 320px at 50% -80px, rgba(139,92,246,0.18), transparent 70%), radial-gradient(800px 400px at 80% 20%, rgba(88,28,135,0.10), transparent 70%)',
        }}
      />

      {/* ---------------------------------------------------------- header */}
      <header className="relative z-10 border-b border-white/5">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-4 sm:px-6">
          <div className="flex items-center gap-3">
            <img src="/logo.png" alt="AegisVPN logo" className="h-9 w-9 rounded-xl" />
            <span className="text-lg font-semibold tracking-tight">
              Aegis<span className="text-violet-400">VPN</span>
            </span>
          </div>
          <div className="flex items-center gap-3">
            <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-400">
              v1.2.1
            </Badge>
            <a
              href="https://github.com/shadow7019/AegisVPN"
              target="_blank"
              rel="noreferrer"
              className="hidden text-sm text-zinc-400 transition-colors hover:text-white sm:block"
            >
              Source & docs ↗
            </a>
          </div>
        </div>
      </header>

      <main className="relative z-10 flex-1">
        {/* ------------------------------------------------------------ hero */}
        <section className="mx-auto max-w-6xl px-4 pb-14 pt-16 text-center sm:px-6">
          <img
            src="/logo-full.png"
            alt="AegisVPN shield logo"
            width={200}
            height={200}
            className="mx-auto mb-8 h-40 w-40 rounded-3xl shadow-[0_0_80px_rgba(139,92,246,0.35)]"
          />
          <h1 className="mx-auto max-w-3xl text-4xl font-bold tracking-tight sm:text-5xl">
            Your digital shield.
            <span className="bg-gradient-to-r from-violet-400 to-purple-300 bg-clip-text text-transparent"> Everywhere.</span>
          </h1>
          <p className="mx-auto mt-5 max-w-2xl text-balance text-lg text-zinc-400">
            A real WireGuard-based VPN — not a mock. Kill Switch, Double VPN, Threat Protection,
            Split Tunneling and an <strong className="text-zinc-200">80-node fleet across 56 countries</strong>.
          </p>
          <div className="mt-8 flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-sm text-zinc-500">
            <span className="flex items-center gap-1.5"><ShieldCheck className="h-4 w-4 text-violet-400" /> MIT licensed</span>
            <span className="flex items-center gap-1.5"><Server className="h-4 w-4 text-violet-400" /> 80 nodes · 56 countries</span>
            <span className="flex items-center gap-1.5"><Globe2 className="h-4 w-4 text-violet-400" /> No logs</span>
          </div>
        </section>

        {/* -------------------------------------------------------- downloads */}
        <section aria-labelledby="downloads-heading" className="mx-auto max-w-6xl px-4 pb-16 sm:px-6">
          <h2 id="downloads-heading" className="sr-only">Download AegisVPN</h2>
          <div className="grid gap-5 md:grid-cols-3">
            {PLATFORMS.map((p) => (
              <Card
                key={p.id}
                className="group relative overflow-hidden border-white/10 bg-white/[0.03] transition-all hover:border-violet-500/40 hover:bg-white/[0.05]"
              >
                <CardHeader className="pb-3">
                  <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-600/30 to-purple-500/10 text-violet-300 ring-1 ring-inset ring-white/10">
                    <Icon kind={p.icon} />
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <CardTitle className="text-xl">{p.name}</CardTitle>
                    <Badge className="bg-violet-600/20 text-violet-300 hover:bg-violet-600/30">{p.badge}</Badge>
                    {userOS === p.id && (
                      <Badge className="bg-emerald-600/25 text-emerald-300 hover:bg-emerald-600/35">Your device</Badge>
                    )}
                  </div>
                  <CardDescription className="text-zinc-500">{p.desc}</CardDescription>
                </CardHeader>
                <CardContent className="mt-auto">
                  <Button
                    asChild
                    className="w-full bg-violet-600 text-white transition-colors hover:bg-violet-500"
                    size="lg"
                  >
                    <a href={`/downloads/${p.file}`} download>
                      <Download className="mr-2 h-4 w-4" />
                      Download · {p.size}
                    </a>
                  </Button>
                  <p className="mt-3 text-xs leading-relaxed text-zinc-500">{p.note}</p>
                </CardContent>
              </Card>
            ))}
          </div>

          {/* app won't open? — security screens explained */}
          <div className="mt-8 rounded-2xl border border-amber-500/30 bg-amber-500/[0.05] p-5">
            <div className="flex items-center gap-2">
              <MousePointerClick className="h-4 w-4 text-amber-400" aria-hidden />
              <p className="font-medium text-amber-200">App won&rsquo;t open after download? It&rsquo;s the security screen — not a broken file.</p>
            </div>
            <p className="mt-1.5 text-sm leading-relaxed text-zinc-400">
              AegisVPN ships without paid code-signing certificates (Apple: $99/yr, Windows EV: ~$300/yr), so every
              OS shows a <strong className="text-zinc-200">one-time warning for non-store apps</strong>. The download
              itself is verified intact — check with the file-drop tester below. Match your device for the exact clicks:
            </p>
            <div className="mt-4 grid gap-4 md:grid-cols-3">
              {SEC_FIXES.map((s) => (
                <div
                  key={s.os}
                  className={`rounded-xl border p-4 ${userOS === s.os ? 'border-emerald-500/40 bg-emerald-500/[0.06]' : 'border-white/10 bg-white/[0.02]'}`}
                >
                  <div className="flex items-center gap-2">
                    <h3 className="font-semibold">{s.label}</h3>
                    {userOS === s.os && (
                      <Badge className="bg-emerald-600/25 text-emerald-300 hover:bg-emerald-600/35">Your device</Badge>
                    )}
                  </div>
                  <p className="mt-1 text-xs font-medium text-amber-300/90">You will see: {s.dialog}</p>
                  <ol className="mt-2.5 space-y-1.5 text-xs leading-relaxed text-zinc-400">
                    {s.steps.map((step, i) => (
                      <li key={i} className="flex gap-2">
                        <span className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold ${userOS === s.os ? 'bg-emerald-600/30 text-emerald-300' : 'bg-violet-600/30 text-violet-300'}`}>{i + 1}</span>
                        <span>{step}</span>
                      </li>
                    ))}
                  </ol>
                </div>
              ))}
            </div>
          </div>

          {/* macOS: one-line installer (no Gatekeeper) */}
          <div className="mt-6 rounded-2xl border border-emerald-500/25 bg-emerald-500/[0.05] p-5">
            <div className="flex items-center gap-2">
              <Zap className="h-4 w-4 text-emerald-400" aria-hidden />
              <p className="font-medium">macOS — recommended: one-line install (zero dialogs)</p>
            </div>
            <p className="mt-1.5 text-sm leading-relaxed text-zinc-400">
              Browser downloads get a macOS quarantine stamp — that is what triggers the
              &ldquo;Apple could not verify&rdquo; dialog on DMG installs. This installer (same approach
              as Tailscale / Homebrew) downloads <strong className="text-zinc-200">without quarantine</strong>, so
              the app opens with <strong className="text-zinc-200">no Gatekeeper prompt at all</strong>.
              Copy into <strong className="text-zinc-200">Terminal</strong> and press Return:
            </p>
            {origin ? (
              <CopyCommand cmd={`curl -fsSL ${origin}/downloads/install-macos.sh | bash -s -- "${origin}"`} />
            ) : (
              <div className="mt-2 rounded-xl border border-white/10 bg-black/50 px-3 py-2 font-mono text-xs text-zinc-500">loading command…</div>
            )}
            <p className="mt-2.5 text-xs leading-relaxed text-zinc-500">
              What it does: downloads the signed universal app → verifies SHA-256 → installs to
              <code className="mx-1 rounded bg-black/40 px-1 py-0.5 font-mono">/Applications</code>→ launches it.
              Approve the admin password prompt once (the VPN tunnel needs root) — the dashboard
              opens at <code className="rounded bg-black/40 px-1 py-0.5 font-mono">http://127.0.0.1:8080</code> (localhost only).
            </p>
          </div>

          {/* macOS: manual DMG path + Gatekeeper pass */}
          <div className="mt-4 rounded-2xl border border-violet-500/20 bg-violet-500/[0.04] p-5">
            <p className="font-medium">Manual install (DMG) — includes the one-time Gatekeeper pass</p>
            <ol className="mt-3 space-y-2 text-sm text-zinc-400">
              <li className="flex gap-3">
                <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-violet-600/30 text-xs font-semibold text-violet-300">1</span>
                <span>Download <strong className="text-zinc-200">AegisVPN-1.2.1.dmg</strong> and open it. The filename matters: v1.2.0 files some browsers cached were broken — v1.2.1 is a new file, so a re-download is guaranteed fresh. Drag <strong className="text-zinc-200">AegisVPN.app</strong> onto <strong className="text-zinc-200">Applications</strong>, click <em>Done</em> on the dialog.</span>
              </li>
              <li className="flex gap-3">
                <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-violet-600/30 text-xs font-semibold text-violet-300">2</span>
                <span>Open <strong className="text-zinc-200">Terminal</strong> (Spotlight → type &ldquo;Terminal&rdquo;), paste this and press Return:</span>
              </li>
            </ol>
            <CopyCommand cmd="xattr -dr com.apple.quarantine /Applications/AegisVPN.app" />
            <p className="mt-2.5 text-xs leading-relaxed text-zinc-500">
              No Terminal? Open <strong className="text-zinc-400">System Settings → Privacy &amp; Security</strong>, scroll to <em>Open Anyway</em> next to
              &ldquo;AegisVPN was blocked&rdquo; → <em>Open Anyway</em> → <em>Open</em>. (macOS 13/14 also supports right-click → Open.)
            </p>
            <p className="mt-3 text-xs leading-relaxed text-zinc-500">
              Still not opening after the xattr step? Run the engine directly in Terminal — it prints
              exactly what is happening instead of failing silently:
            </p>
            <CopyCommand cmd="/Applications/AegisVPN.app/Contents/MacOS/AegisVPN" />
            <p className="mt-2 text-xs leading-relaxed text-zinc-500">
              A healthy start ends with <code className="rounded bg-black/40 px-1 py-0.5 font-mono">aegisvpn dashboard on http://127.0.0.1:8080</code> and opens the dashboard in your browser.
            </p>
            <ol className="mt-3 space-y-2 text-sm text-zinc-400" start={3}>
              <li className="flex gap-3">
                <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-violet-600/30 text-xs font-semibold text-violet-300">3</span>
                <span>Launch AegisVPN — approve the admin prompt (the utun device needs root, asked once) and the dashboard opens at <code className="rounded bg-black/40 px-1.5 py-0.5 font-mono text-xs">http://127.0.0.1:8080</code>. Gatekeeper never asks again.</span>
              </li>
            </ol>
          </div>

          {/* macOS: why is the app only ~15 MB */}
          <div className="mt-4 rounded-2xl border border-white/10 bg-white/[0.02] p-5">
            <p className="font-medium">Why is the macOS app &ldquo;only&rdquo; ~15 MB?</p>
            <p className="mt-2 text-sm leading-relaxed text-zinc-400">
              That is the correct size — small here means <strong className="text-zinc-200">native, not incomplete</strong>.
              AegisVPN is a Go build of the wireguard-go core (the same engine behind NordVPN&apos;s NordLynx)
              with the dashboard and the 80-node registry compiled in — <strong className="text-zinc-200">no bundled browser runtime</strong>.
              The 150–200&nbsp;MB installers you have seen elsewhere are Electron apps: they ship an entire
              Chromium copy. NordVPN&apos;s own macOS app is ~60&nbsp;MB; ours unpacks to a ~33&nbsp;MB app.
              Nothing is missing — verify the SHA-256 below against
              <code className="mx-1 rounded bg-black/40 px-1 py-0.5 font-mono">SHA256SUMS.txt</code> if in doubt.
            </p>
          </div>

          {/* portable + checksums */}
          <div className="mt-6 flex flex-col items-start justify-between gap-4 rounded-2xl border border-white/10 bg-white/[0.02] p-5 sm:flex-row sm:items-center">
            <div>
              <p className="font-medium">Portable edition (no install required)</p>
              <p className="text-sm text-zinc-500">AegisVPN-win-x64-portable.zip · 8.6 MB — unpack and run.</p>
            </div>
            <Button asChild variant="outline" className="border-white/15 bg-transparent hover:bg-white/10">
              <a href="/downloads/AegisVPN-win-x64-portable.zip" download>
                <Download className="mr-2 h-4 w-4" /> Download ZIP
              </a>
            </Button>
          </div>

          {/* macOS zip alternative */}
          <div className="mt-4 flex flex-col items-start justify-between gap-4 rounded-2xl border border-white/10 bg-white/[0.02] p-5 sm:flex-row sm:items-center">
            <div>
              <p className="font-medium">macOS alternative: plain ZIP of the signed app</p>
              <p className="text-sm text-zinc-500">AegisVPN-macos.zip · 14.7 MB — unpack, drag to Applications, pass Gatekeeper once (same xattr step as the DMG).</p>
            </div>
            <Button asChild variant="outline" className="border-white/15 bg-transparent hover:bg-white/10">
              <a href="/downloads/AegisVPN-macos.zip" download>
                <Download className="mr-2 h-4 w-4" /> Download ZIP
              </a>
            </Button>
          </div>

          {/* verify */}
          <div className="mt-6 rounded-2xl border border-white/10 bg-white/[0.02] p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="font-medium">Verify your download — drop the file here</p>
              <Button
                variant="outline"
                size="sm"
                onClick={loadHashes}
                className="border-white/15 bg-transparent hover:bg-white/10"
              >
                {loadingHashes ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="mr-2 h-3.5 w-3.5" />}
                {hashes ? 'Refresh' : 'Show checksums'}
              </Button>
            </div>
            <div className="mt-3">
              <DropZone />
            </div>
            {hashes && (
              <div className="mt-3 max-h-44 overflow-y-auto rounded-xl bg-black/40 p-3 pr-1 aegis-scroll">
                {hashes.map(([f, h]) => (
                  <ChecksumRow key={f} file={f} hash={h} />
                ))}
              </div>
            )}
            {!hashes && (
              <p className="mt-2 text-xs text-zinc-500">
                Compare against SHA256SUMS.txt before installing — run <code className="rounded bg-black/40 px-1.5 py-0.5 font-mono">sha256sum -c SHA256SUMS.txt</code> after download.
              </p>
            )}
          </div>
        </section>

        {/* --------------------------------------------------------- features */}
        <section aria-labelledby="features-heading" className="mx-auto max-w-6xl px-4 pb-20 sm:px-6">
          <h2 id="features-heading" className="text-center text-2xl font-bold tracking-tight sm:text-3xl">
            Everything you expect from a premium VPN
          </h2>
          <div className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {FEATURES.map((f) => (
              <div
                key={f.title}
                className="rounded-2xl border border-white/10 bg-white/[0.02] p-5 transition-colors hover:border-violet-500/30"
              >
                <f.icon className="mb-3 h-5 w-5 text-violet-400" aria-hidden />
                <h3 className="font-semibold">{f.title}</h3>
                <p className="mt-1 text-sm leading-relaxed text-zinc-500">{f.desc}</p>
              </div>
            ))}
          </div>
        </section>
      </main>

      {/* ---------------------------------------------------------- footer */}
      <footer className="relative z-10 mt-auto border-t border-white/5 pb-[env(safe-area-inset-bottom)]">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-2 px-4 py-6 text-sm text-zinc-500 sm:flex-row sm:px-6">
          <div className="flex items-center gap-2">
            <img src="/logo-192.png" alt="" className="h-5 w-5 rounded-md" />
            <span>© 2025 AegisVPN Project · MIT License</span>
          </div>
          <a href="https://github.com/shadow7019/AegisVPN" target="_blank" rel="noreferrer" className="transition-colors hover:text-zinc-300">
            github.com/shadow7019/AegisVPN ↗
          </a>
        </div>
      </footer>
    </div>
  )
}
