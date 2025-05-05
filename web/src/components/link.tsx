import type { AnchorProps } from '@solidjs/router'
import { A } from '@solidjs/router'
import type { Component } from 'solid-js'

export const Link: Component<AnchorProps> = props => {
	const onClick = (e: MouseEvent) => {
		const targetUrl = new URL(props.href, window.location.toString())
		const currentUrl = new URL(window.location.toString())
		const isExternal =
			targetUrl.protocol !== currentUrl.protocol ||
			targetUrl.host !== currentUrl.host

		if (isExternal) {
			e.preventDefault()
			return window.Telegram.WebApp.openLink('t.me/mini_hub_bot/app')
		}
	}

	return (
		<A {...props} onClick={onClick} class={props.class}>
			{props.children}
		</A>
	)
}
