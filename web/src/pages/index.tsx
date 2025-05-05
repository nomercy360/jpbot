import { createSignal, Show } from 'solid-js'
import { getLeaderboard } from '~/lib/api'
import type { LeaderboardEntry, LeaderboardResponse } from '~/lib/api'

type TimePeriod = 'daily' | 'weekly' | 'monthly';

export default function Leaderboard() {
	const [leaderboard, setLeaderboard] = createSignal<LeaderboardResponse | null>(null)
	const [selectedPeriod, setSelectedPeriod] = createSignal<TimePeriod>('daily')
	const [error, setError] = createSignal<string | null>(null)
	const [loading, setLoading] = createSignal(true)

	const fetchLeaderboard = async () => {
		setLoading(true)
		setError(null)
		const { data, error: err } = await getLeaderboard()
		if (err) {
			setError(err)
		} else {
			setLeaderboard(data)
		}
		setLoading(false)
	}

	fetchLeaderboard()

	const getCurrentEntries = () => {
		const data = leaderboard()
		if (!data) return []
		return data[selectedPeriod()]
	}

	return (
		<div class="mx-auto px-2 py-4">
			<h1 class="text-3xl font-bold mb-6">
				Таблица лидеров
			</h1>

			<div class="px-2 flex space-x-1.5 mb-6">
				{(['daily', 'weekly', 'monthly'] as TimePeriod[]).map((period) => {
					const labelMap: Record<TimePeriod, string> = {
						daily: 'Сегодня',
						weekly: 'Неделя',
						monthly: 'Месяц',
					}
					return (
						<button
							class={`px-4 py-2 rounded-lg text-xs font-medium transition-colors ${
								selectedPeriod() === period
									? 'bg-primary text-primary-foreground'
									: 'bg-muted text-muted-foreground'
							}`}
							onClick={() => setSelectedPeriod(period)}
						>
							{labelMap[period]}
						</button>
					)
				})}
			</div>

			<Show when={error()}>
				<div class="bg-destructive/10 text-destructive p-4 rounded-lg mb-4">
					{error()}
				</div>
			</Show>

			<Show when={loading()}>
				<div class="text-center py-8">Loading...</div>
			</Show>

			<Show when={!loading() && !error()}>
				<div class="bg-card rounded-t-lg shadow-sm">
					<div class="overflow-x-auto">
						<table class="w-full">
							<thead>
							<tr class="text-sm border-b border-border">
								<th class="text-left px-2 py-2">Место</th>
								<th class="text-left px-2 py-2">Пользователь</th>
								<th class="text-left px-2 py-2">Уровень</th>
								<th class="text-right px-2 py-2">Очки</th>
							</tr>
							</thead>
							<tbody>
							{getCurrentEntries().map((entry) => (
								<tr class="border-b border-border last-of-type:border-b-0">
									<td class="px-2 py-2 font-bold">#{entry.rank}</td>
									<td class="px-2 py-2">
										<div class="flex items-center space-x-2">
											<img
												src={entry.avatar_url}
												alt={entry.username}
												class="size-7 rounded-full"
											/>
											<div>
												<div class="font-medium">
													{entry.first_name} {entry.last_name}
												</div>
												<div class="text-sm text-muted-foreground">
													@{entry.username}
												</div>
											</div>
										</div>
									</td>
									<td class="px-2 py-2">{entry.level}</td>
									<td class="px-3 py-2 text-right font-bold">{entry.score}</td>
								</tr>
							))}
							</tbody>
						</table>
					</div>
				</div>
			</Show>
		</div>
	)
}
