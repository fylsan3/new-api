/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { IconBadge } from '@/components/ui/icon-badge'

/** Bordered panel with an icon title, shared by the statistics sections. */
export function Section(props: {
  icon: React.ReactNode
  title: string
  description?: string
  actions?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex items-start justify-between gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
        <div className='flex items-center gap-2'>
          <IconBadge tone='info' size='sm'>
            {props.icon}
          </IconBadge>
          <div>
            <div className='text-sm font-semibold'>{props.title}</div>
            {props.description && (
              <div className='text-muted-foreground text-xs'>
                {props.description}
              </div>
            )}
          </div>
        </div>
        {props.actions}
      </div>
      {props.children}
    </div>
  )
}
