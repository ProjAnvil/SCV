# order-service (perception fixture)

A deliberately dense Spring Boot 3 / Java 21 service used to exercise SCV's
code perception. It is not meant to compile against a real classpath; it exists
so the analyzer sees every injection style in one tree.

Injection styles covered on purpose:

- constructor injection (the common case)
- field `@Autowired` injection
- setter `@Autowired` injection
- `@Bean`-method injection in a `@Configuration` class
- `@Qualifier` narrowing two implementations
- `@Primary` disambiguation
- collection injection (`List<Mailer>`, `Map<String, Mailer>`)
- `Optional<Mailer>` and `@Lazy` dependencies
- `@Value` on a constructor parameter

Cycles covered on purpose:

- `OrderService -> PricingService -> OrderService` via **constructor**:
  Spring cannot satisfy this, so SCV must grade it `fatal`.
- `NotificationService -> AuditService -> NotificationService` via **field**:
  the container tolerates this, so SCV must grade it `tolerated`.
