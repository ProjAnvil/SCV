# Codebones skeleton — mini-orders

```
com.mini.Application (Application.java)
  @SpringBootApplication

com.mini.OrderController (OrderController.java)
  deps: OrderService
  endpoints: GET /orders/{id}

com.mini.OrderService (OrderService.java)
  deps: OrderRepository
  methods:
    describe(id) -> repository.summary(id)

com.mini.OrderRepository (OrderRepository.java)
  methods:
    summary(id) -> "row(" + id + ")"
```
