package org.thirdparty.compat;

import com.acme.core.GreetingAudit;
import org.springframework.stereotype.Service;

// torture: OUTSIDE component scan (main class package is com.acme, this is org.thirdparty).
// Runtime: never instantiated. Static analysis: unavoidable false positive.
@Service
public class LegacyCompatService {

    private final GreetingAudit audit;

    public LegacyCompatService(GreetingAudit audit) {
        this.audit = audit;
    }

    public void compat() {
        audit.audit("legacy compat invoked");
    }
}
