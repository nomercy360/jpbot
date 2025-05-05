import { store } from '~/store'

export default function Profile() {
	return (
		<div class="p-2">
			<div class="bg-card rounded-lg shadow-sm p-4">
				<div class="flex items-center space-x-4 mb-6">
					<img
						src={store.user.avatar_url || 'https://assets.peatch.io/avatars/default.svg'}
						alt={store.user.username}
						class="w-20 h-20 rounded-full"
					/>
					<div>
						<h1 class="text-2xl font-bold">
							{store.user.first_name} {store.user.last_name}
						</h1>
						<p class="text-muted-foreground">@{store.user.username}</p>
					</div>
				</div>

				<div class="grid grid-cols-3 gap-4">
					<div class="bg-accent/50 p-4 rounded-lg">
						<p class="text-sm text-muted-foreground">Уровень японского</p>
						<p class="text-lg font-semibold">{store.user.level}</p>
					</div>
					<div class="bg-accent/50 p-4 rounded-lg">
						<p class="text-sm text-muted-foreground">Заработанные очки</p>
						<p class="text-lg font-semibold">{store.user.points}</p>
					</div>
					<div class="bg-accent/50 p-4 rounded-lg">
						<p class="text-sm text-muted-foreground">Выполнено заданий</p>
						<p class="text-lg font-semibold">{store.user.exercises_done}</p>
					</div>
				</div>

				<div class="mt-6 pt-6 border-t border-border px-2">
					<h2 class="text-lg font-semibold mb-4">Информация о пользователе</h2>
					<div class="space-y-1">
						<div class="flex justify-between">
							<span class="text-muted-foreground">Username</span>
							<span>@{store.user.username}</span>
						</div>
						<div class="flex justify-between">
							<span class="text-muted-foreground">Участник с</span>
							<span>{new Date(store.user.created_at).toLocaleDateString('ru-RU')}</span>
						</div>
					</div>
				</div>
			</div>
		</div>
	)
}
