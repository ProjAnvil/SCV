package com.b2m.domain;

import org.springframework.stereotype.Repository;

@Repository
public class CustomerRepository {
    public String find(long id) {
        return "customer-" + id;
    }
}
