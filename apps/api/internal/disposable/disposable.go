// Package disposable recognises throwaway email providers, whose inboxes
// anyone can read or which vanish after minutes. Hosted Bridge refuses
// sign-ups from them (BRIDGE_BLOCK_DISPOSABLE_EMAIL).
//
// The list is deliberately small: the most common public services, not every
// domain they ever used. Privacy relays that forward to a real inbox
// (Firefox Relay, DuckDuckGo, SimpleLogin, addy.io, iCloud Hide My Email) are
// not disposable and are not listed.
package disposable

import "strings"

// Email reports whether the address's domain, or a domain it is under, is a
// known disposable email provider.
func Email(addr string) bool {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return false
	}
	return Domain(addr[at+1:])
}

// Domain reports whether domain, or a parent of it, is a known disposable
// email provider. Matching ignores case and a trailing dot.
func Domain(domain string) bool {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	for d != "" {
		if _, ok := domains[d]; ok {
			return true
		}
		dot := strings.IndexByte(d, '.')
		if dot < 0 {
			return false
		}
		d = d[dot+1:]
	}
	return false
}

// Count is the number of listed domains.
func Count() int { return len(domains) }

var domains = func() map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, d := range list {
		m[d] = struct{}{}
	}
	return m
}()

var list = []string{
	// Mailinator and its alternate domains.
	"mailinator.com", "mailinator.net", "mailinator2.com", "mailinater.com", "notmailinator.com",
	"binkmail.com", "bobmail.info", "chammy.info", "devnullmail.com", "letthemeatspam.com",
	"reallymymail.com", "reconmail.com", "safetymail.info", "sogetthis.com", "spamhereplease.com",
	"spamthisplease.com", "streetwisemail.com", "suremail.info", "thisisnotmyrealemail.com",
	"tradermail.info", "veryrealemail.com", "zippymail.info", "mailin8r.com", "mailmetrash.com",
	"sendspamhere.com", "spamherelots.com", "supergreatmail.com",
	// Guerrilla Mail.
	"guerrillamail.com", "guerrillamail.net", "guerrillamail.org", "guerrillamail.biz",
	"guerrillamail.de", "guerrillamail.info", "guerrillamailblock.com", "grr.la", "sharklasers.com",
	"pokemail.net", "spam4.me",
	// 10 Minute Mail and similar timers.
	"10minutemail.com", "10minutemail.net", "10minutemail.co.uk", "10minutemail.de", "10minmail.com",
	"20minutemail.com", "20minutemail.it", "zehnminutenmail.de", "10mail.org",
	// Temp Mail and look-alikes.
	"temp-mail.org", "temp-mail.io", "temp-mail.ru", "tempmail.com", "tempmail.net", "tempmail.dev",
	"tempmailo.com", "tempail.com", "tempinbox.com", "tempr.email", "tempmailaddress.com",
	"temporary-mail.net", "tempomail.fr", "temporaryemail.net", "tempemail.net", "mytemp.email",
	"mytempemail.com", "mail-temporaire.fr",
	// YOPmail and its alternate domains.
	"yopmail.com", "yopmail.net", "yopmail.fr", "cool.fr.nf", "jetable.fr.nf", "nospam.ze.tc",
	"nomail.xl.cx", "mega.zik.dj", "speed.1s.fr", "courriel.fr.nf", "moncourrier.fr.nf",
	"monemail.fr.nf", "monmail.fr.nf",
	// TrashMail and its alternate domains.
	"trashmail.com", "trashmail.net", "trashmail.de", "trashmail.at", "trashmail.me", "trashmail.io",
	"trashmail.ws", "trash-mail.com", "trash-mail.at", "trashmailer.com", "wegwerfmail.de",
	"wegwerfmail.net", "wegwerfmail.org", "kurzepost.de", "objectmail.com", "proxymail.eu", "rcpt.at",
	"mytrashmail.com", "mt2015.com",
	// 1secmail.
	"1secmail.com", "1secmail.net", "1secmail.org", "esiix.com", "wwjmp.com", "xojxe.com",
	"yoggm.com", "oosln.com", "vddaz.com", "dpptd.com", "kzccv.com", "qiott.com", "wuuvo.com",
	"icznn.com", "ezztt.com", "vjuum.com", "laafd.com", "txcct.com", "rteet.com",
	// Dropmail.
	"dropmail.me", "emltmp.com", "emlpro.com", "emlhub.com", "yomail.info", "spymail.one",
	"freeml.net", "mailpwr.com", "mimimail.me",
	// Fake Mail Generator.
	"armyspy.com", "cuvox.de", "dayrep.com", "einrot.com", "fleckens.hu", "gustr.com",
	"jourrapide.com", "rhyta.com", "superrito.com", "teleworm.us",
	// Other public throwaway inboxes.
	"throwawaymail.com", "throwam.com", "dispostable.com", "discard.email", "discardmail.com",
	"discardmail.de", "spambog.com", "spambog.de", "spambog.ru", "spamgourmet.com", "spamex.com",
	"spam.la", "spamfree24.org", "spamhole.com", "spaml.com", "spaml.de", "mailnesia.com",
	"mailcatch.com", "maildrop.cc", "mailnull.com", "mailexpire.com", "mailforspam.com",
	"mailmoat.com", "mintemail.com", "mohmal.com", "moakt.com", "moakt.cc", "emailondeck.com",
	"emailfake.com", "fakeinbox.com", "fakemailgenerator.com", "getairmail.com", "getnada.com",
	"nada.email", "inboxkitten.com", "harakirimail.com", "incognitomail.com", "incognitomail.org",
	"jetable.org", "jetable.com", "jetable.net", "mailsac.com", "burnermail.io", "33mail.com",
	"mailpoof.com", "mailtothis.com", "meltmail.com", "nowmymail.com", "pookmail.com",
	"quickinbox.com", "rmqkr.net", "selfdestructingmail.com", "slopsbox.com", "smellfear.com",
	"sofort-mail.de", "spamavert.com", "spambox.us", "spamcero.com", "spamcorptastic.com",
	"spamday.com", "spamspot.com", "spamthis.co.uk", "anonbox.net", "anonymbox.com", "antispam.de",
	"boximail.com", "deadaddress.com", "despam.it", "disposableemailaddress.com", "dodgeit.com",
	"dodgit.com", "e4ward.com", "emailias.com", "emailsensei.com", "ephemail.net", "filzmail.com",
	"gishpuppy.com", "haltospam.com", "hidemail.de", "ieatspam.eu", "ieatspam.info", "imails.info",
	"inboxalias.com", "instant-mail.de", "jnxjn.com", "kasmail.com", "koszmail.pl", "lookugly.com",
	"lroid.com", "mail1a.de", "mailbidon.com", "maileater.com", "mailscrap.com", "mailslite.com",
	"mailtome.de", "mailzilla.org", "mbx.cc", "messagebeamer.de", "mierdamail.com", "nepwk.com",
	"nervmich.net", "nervtmich.net", "netmails.net", "netzidiot.de", "nobulk.com", "noclickemail.com",
	"nogmailspam.info", "nomail2me.com", "nospamfor.us", "nospammail.net", "nurfuerspam.de",
	"oneoffemail.com", "oopi.org", "pjjkp.com", "recode.me", "recyclemail.dk", "regbypass.com",
	"rejectmail.com", "rtrtr.com", "shiftmail.com", "shortmail.net", "skeefmail.com", "smashmail.de",
	"spamobox.com", "spamslicer.com", "spamtrail.com", "thanksnospam.info", "twinmail.de",
	"wh4f.org", "whyspam.me", "willselfdestruct.com", "wronghead.com", "wuzupmail.net", "xemaps.com",
	"xents.com", "xmaily.com", "xoxy.net", "yep.it", "yuurok.com", "zoemail.org",
}
