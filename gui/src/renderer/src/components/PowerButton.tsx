import { motion, useReducedMotion } from 'framer-motion'
import clsx from 'clsx'

interface Props {
  connected: boolean
  waiting: boolean
  stopped: boolean
}

export function PowerButton({ connected, waiting, stopped }: Props) {
  const reduceMotion = useReducedMotion()
  const iconPath = 'M18.364 4.636a9 9 0 1 1-12.728 0M12 2v8'
  const statusText = stopped ? '已停止' : connected ? '已连接' : '未连接设备'

  return (
    <div className="flex flex-col items-center gap-4">
      <motion.button
        aria-label={`${statusText}，${stopped ? '启动' : '停止'} BetterTether`}
        className={clsx(
          'relative w-32 h-32 rounded-full flex items-center justify-center cursor-pointer no-drag',
          'bg-zinc-900/80 backdrop-blur-xl border-2',
          stopped
            ? 'border-zinc-700/50 animate-glow-off'
            : waiting
              ? 'border-amber-400/50'
              : 'border-emerald-500/50 animate-glow-on'
        )}
        whileHover={reduceMotion ? undefined : { scale: 1.05 }}
        whileTap={reduceMotion ? undefined : { scale: 0.95 }}
        onClick={() => {
          if (!stopped) {
            window.bettertether.stopDaemon()
          } else {
            window.bettertether.startDaemon()
          }
        }}
      >
        {waiting && <span aria-hidden="true" className="pointer-events-none absolute inset-0 rounded-full animate-glow-waiting" />}

        <motion.svg
          viewBox="0 0 24 24"
          className={clsx('w-14 h-14', connected ? 'text-emerald-400' : waiting ? 'text-amber-400' : 'text-zinc-500')}
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          animate={{ scale: connected && !reduceMotion ? [1, 1.1, 1] : 1 }}
          transition={{ duration: 2, repeat: connected && !reduceMotion ? Infinity : 0, ease: 'easeInOut' }}
        >
          <path d={iconPath} />
        </motion.svg>
      </motion.button>
      <p role="status" className={clsx('text-xs', waiting ? 'text-amber-400/90' : 'text-zinc-400')}>
        {statusText}
      </p>
    </div>
  )
}
