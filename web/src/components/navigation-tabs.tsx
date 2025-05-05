import { Link } from '~/components/link'
import { useLocation } from '@solidjs/router'
import { cn } from '~/lib/utils'

export default function NavigationTabs(props: any) {
	const location = useLocation()
	return (
		<>
			<div class="fixed bottom-0 left-0 right-0 bg-card border-t border-border">
				<div class="container mx-auto px-4">
					<div class="flex justify-around py-3">
						<Link
							href="/"
							class={cn('w-full flex flex-col items-center space-y-1 text-neutral-400', location.pathname === '/' && 'text-white')}
						>
							<svg
								xmlns="http://www.w3.org/2000/svg"
								width="24"
								height="24"
								viewBox="0 0 24 24"
								stroke="currentColor"
								stroke-width="2"
								stroke-linecap="round"
								stroke-linejoin="round"
								class="size-6"
							>
								<path d="M6 9H4.5a2.5 2.5 0 0 1 0-5H6" />
								<path d="M18 9h1.5a2.5 2.5 0 0 0 0-5H18" />
								<path d="M4 22h16" />
								<path d="M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22" />
								<path d="M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22" />
								<path d="M18 2H6v7a6 6 0 0 0 12 0V2Z" />
							</svg>
							<span class="text-sm">Рейтинг</span>
						</Link>
						<Link
							href="/profile"
							class={cn('w-full flex flex-col items-center space-y-1 text-neutral-400', location.pathname === '/profile' && 'text-white')}
						>
							<svg
								xmlns="http://www.w3.org/2000/svg"
								width="24"
								height="24"
								viewBox="0 0 24 24"
								stroke="currentColor"
								stroke-width="2"
								stroke-linecap="round"
								stroke-linejoin="round"
								class="size-7"
							>
								<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
								<circle cx="12" cy="7" r="4" />
							</svg>
							<span class="text-sm">Профиль</span>
						</Link>
					</div>
				</div>
			</div>
			{props.children}
		</>
	)
}
