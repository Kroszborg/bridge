package dev.bridge.gateway.account

import dev.bridge.gateway.net.BillingDto
import java.util.Locale

/** One plan limit as shown in the app: "2 of 3 phones". */
data class UsageLine(val label: String, val used: Int, val limit: Int?) {
    val unlimited: Boolean get() = limit == null

    /** 0 to 1 of the limit used; null when unlimited. */
    val fraction: Float? get() = limit?.let { if (it <= 0) 1f else (used.toFloat() / it).coerceIn(0f, 1f) }

    val atLimit: Boolean get() = limit != null && used >= limit

    val text: String get() = if (limit == null) "$used · unlimited" else "$used of $limit"
}

/** The billing response, reduced to what the plan card shows. */
data class PlanSummary(
    val title: String,
    val price: String?,
    val selfHosted: Boolean,
    val status: String?,
    val lines: List<UsageLine>,
) {
    /** Any limit is reached, so upgrading would unblock something. */
    val anyAtLimit: Boolean get() = lines.any { it.atLimit }

    companion object {
        fun from(billing: BillingDto): PlanSummary {
            val plan = billing.plan
            if (!billing.enabled || plan == null || plan.id == "self_hosted") {
                return PlanSummary("Self-hosted · no limits", null, selfHosted = true, status = null, lines = emptyList())
            }
            val u = billing.usage
            val l = plan.limits
            return PlanSummary(
                title = plan.name,
                price = price(plan.priceCents, plan.currency),
                selfHosted = false,
                status = billing.status.takeIf { it.isNotBlank() && it != "active" && it != "none" },
                lines = listOf(
                    UsageLine("Live messages this period", u.liveMessages, l.liveMessages),
                    UsageLine("Phones", u.phones, l.phones),
                    UsageLine("Projects", u.projects, l.projects),
                    UsageLine("Members", u.members, l.members),
                ),
            )
        }

        fun price(cents: Int, currency: String): String {
            if (cents <= 0) return "Free"
            val symbol = if (currency.equals("USD", ignoreCase = true)) "$" else "${currency.uppercase()} "
            val amount = if (cents % 100 == 0) "${cents / 100}" else String.format(Locale.US, "%.2f", cents / 100.0)
            return "$symbol$amount / month"
        }
    }
}
