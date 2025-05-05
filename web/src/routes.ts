import type { RouteDefinition } from '@solidjs/router'
import NavigationTabs from '~/components/navigation-tabs'
import Leaderboard from '~/pages'
import Profile from '~/pages/profile'


export const routes: RouteDefinition[] = [
	{
		path: '/',
		component: NavigationTabs,
		children: [
			{
				'path': '/',
				'component': Leaderboard,
			},
			{
				'path': '/profile',
				'component': Profile,
			},
		],
	},
]
