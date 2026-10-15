package com.sb.cond;

import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Component;

// §5 conditions wording (v1): Simple(detail=havingValue), Simple, or just
// the simple name.
@Component
@Profile("dev")
class DevBean {
}

@Component
@ConditionalOnProperty(name = "audit.enabled", havingValue = "true")
class CondBean {
}

@Component
@ConditionalOnMissingBean
class MissingBean {
}
