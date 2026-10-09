package dev.bridge.gateway.ui

import androidx.annotation.DrawableRes
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import dev.bridge.gateway.R

/** The signed-in app's sections. Gateway is the original status screen. */
enum class HomeTab(val label: String, @param:DrawableRes val icon: Int) {
    Gateway("Gateway", R.drawable.ic_nav_gateway),
    Messages("Messages", R.drawable.ic_nav_messages),
    Send("Send", R.drawable.ic_nav_send),
    Phones("Phones", R.drawable.ic_nav_phones),
    Account("Account", R.drawable.ic_nav_account),
}

@Composable
fun HomeShell(tab: HomeTab, onTab: (HomeTab) -> Unit, content: @Composable (HomeTab) -> Unit) {
    Scaffold(
        bottomBar = {
            NavigationBar {
                HomeTab.entries.forEach { t ->
                    NavigationBarItem(
                        selected = t == tab,
                        onClick = { onTab(t) },
                        icon = { Icon(painterResource(t.icon), contentDescription = null) },
                        label = { Text(t.label) },
                    )
                }
            }
        },
    ) { inner ->
        // Screens shared with the signed-out app pad themselves for system bars; consuming
        // the Scaffold's insets here keeps them from padding twice.
        Box(Modifier.fillMaxSize().padding(inner).consumeWindowInsets(inner)) { content(tab) }
    }
}
