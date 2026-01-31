import * as React from 'react'
import { cn } from '@/lib/utils'

export interface SliderProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'onChange'> {
  value?: number
  defaultValue?: number
  min?: number
  max?: number
  step?: number
  onChange?: (value: number) => void
}

const Slider = React.forwardRef<HTMLInputElement, SliderProps>(
  ({ className, value, defaultValue = 0.5, min = 0, max = 1, step = 0.01, onChange, ...props }, ref) => {
    const [internalValue, setInternalValue] = React.useState(value ?? defaultValue)

    const currentValue = value ?? internalValue
    const range = max - min
    const percentage = range === 0 ? 0 : ((currentValue - min) / range) * 100

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
      const newValue = parseFloat(e.target.value)
      setInternalValue(newValue)
      onChange?.(newValue)
    }

    return (
      <div className={cn('relative flex w-full touch-none select-none items-center', className)}>
        <div className="relative h-1.5 w-full grow overflow-hidden rounded-full bg-muted">
          <div
            className="absolute h-full bg-primary rounded-full transition-all"
            style={{ width: `${percentage}%` }}
          />
        </div>
        <input
          type="range"
          ref={ref}
          min={min}
          max={max}
          step={step}
          value={currentValue}
          onChange={handleChange}
          className="absolute w-full h-4 opacity-0 cursor-pointer"
          {...props}
        />
        <div
          className="absolute h-3.5 w-3.5 rounded-full border-2 border-primary bg-background ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 pointer-events-none"
          style={{ left: `calc(${percentage}% - 7px)` }}
        />
      </div>
    )
  }
)
Slider.displayName = 'Slider'

export { Slider }
