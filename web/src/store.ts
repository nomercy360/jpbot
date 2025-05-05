import { createStore } from 'solid-js/store'


type User = {
	id: number
	first_name: string
	last_name: string
	username: string
	telegram_id: number
	level: string
	points: number
	exercises_done: number
	current_exercise_id?: number | null
	current_word_id?: number | null
	current_mode: string
	avatar_url?: string | null
	created_at: Date
	updated_at: Date
}

export const [store, setStore] = createStore<{
	user: User
	token: string
}>({
	user: {} as User,
	token: null as any,
})

export const setUser = (user: any) => setStore('user', user)

export const setToken = (token: string) => setStore('token', token)


