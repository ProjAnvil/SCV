package com.sb.lombok;

import lombok.RequiredArgsConstructor;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.stereotype.Component;
import org.springframework.stereotype.Repository;
import org.springframework.stereotype.Service;

// §5 Lombok @RequiredArgsConstructor: final/@NonNull fields become
// lombok-constructor injection points; a @Qualifier on the field narrows
// resolution.
interface Notifier {
}

@Component("qual")
class QualNotifier implements Notifier {
}

@Component
class OtherNotifier implements Notifier {
}

@Repository
class Repo {
}

@Service
@RequiredArgsConstructor
class LombokConsumer {

    private final Repo repo;

    @Qualifier("qual")
    private final Notifier notifier;
}
