import { toast as sonnerToast } from 'vue-sonner'

// Tiny wrapper around vue-sonner that keeps the call sites short
// (`Message.success(...)` -> `toast.success(...)`). Every variant accepts
// either a plain string or `{ title, description, duration }`.

type Payload = string | { title: string, description?: string, duration?: number }

function resolve(p: Payload): [string, { description?: string, duration?: number } | undefined] {
  if (typeof p === 'string') return [p, undefined]
  return [p.title, { description: p.description, duration: p.duration }]
}

export const toast = {
  success: (p: Payload) => {
    const [t, opt] = resolve(p)
    return sonnerToast.success(t, opt)
  },
  error: (p: Payload) => {
    const [t, opt] = resolve(p)
    return sonnerToast.error(t, opt)
  },
  info: (p: Payload) => {
    const [t, opt] = resolve(p)
    return sonnerToast.info(t, opt)
  },
  warning: (p: Payload) => {
    const [t, opt] = resolve(p)
    return sonnerToast.warning(t, opt)
  },
  message: (p: Payload) => {
    const [t, opt] = resolve(p)
    return sonnerToast(t, opt)
  },
  raw: sonnerToast
}

// Friendly aliases so existing code using `Message.xxx` can be search/replaced
// quickly — they all funnel into the same sonner primitives above.
export const Message = {
  success: (s: string) => toast.success(s),
  error:   (s: string) => toast.error(s),
  warning: (s: string) => toast.warning(s),
  info:    (s: string) => toast.info(s)
}
