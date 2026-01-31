import { cn } from '@/lib/utils'
import { ROLES, type RoleConfig } from './spawnFormConstants'

interface RoleSelectorProps {
  selectedRole: string
  onSelectRole: (roleId: string) => void
}

/**
 * Render a responsive grid of selectable role cards.
 *
 * @param selectedRole - The id of the currently selected role.
 * @param onSelectRole - Callback invoked with a role id when a role is selected.
 * @returns A React element containing role cards arranged in a responsive grid.
 */
export function RoleSelector({ selectedRole, onSelectRole }: RoleSelectorProps) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
      {ROLES.map((role) => (
        <RoleCard
          key={role.id}
          role={role}
          isSelected={selectedRole === role.id}
          onClick={() => onSelectRole(role.id)}
        />
      ))}
    </div>
  )
}

interface RoleCardProps {
  role: RoleConfig
  isSelected: boolean
  onClick: () => void
}

/**
 * Render a selectable role button showing the role's icon, name, and description.
 *
 * @param role - Role configuration object containing `id`, `name`, `description`, and `icon` used to render the card
 * @param isSelected - Whether the card is visually presented as selected
 * @param onClick - Click handler invoked when the card is pressed
 * @returns A button element that visually represents the provided role and responds to selection
 */
function RoleCard({ role, isSelected, onClick }: RoleCardProps) {
  const Icon = role.icon

  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex flex-col items-center gap-1.5 p-3 rounded-lg border-2 transition-all',
        'hover:bg-accent hover:border-accent-foreground/20',
        'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
        isSelected
          ? 'border-primary bg-primary/10 text-primary'
          : 'border-border bg-card text-card-foreground'
      )}
    >
      <Icon className={cn('h-5 w-5', isSelected ? 'text-primary' : 'text-muted-foreground')} />
      <span className="text-sm font-medium">{role.name}</span>
      <span className="text-xs text-muted-foreground text-center leading-tight">
        {role.description}
      </span>
    </button>
  )
}