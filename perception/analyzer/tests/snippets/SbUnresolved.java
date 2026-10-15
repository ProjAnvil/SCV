package com.sb.unres;

import org.springframework.stereotype.Component;

// §5 unresolved accounting: a non-bean source type dependency counts as an
// unresolved injection; java.* wants are framework noise (silent).
class External {
}

@Component
class NeedsExternal {

    NeedsExternal(External external) {
    }
}

@Component
class NeedsClock {

    NeedsClock(java.time.ZoneId zone) {
    }
}
