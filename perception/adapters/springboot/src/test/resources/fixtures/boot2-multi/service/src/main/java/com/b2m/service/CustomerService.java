package com.b2m.service;

import com.b2m.domain.CustomerRepository;
import org.springframework.stereotype.Service;

@Service
public class CustomerService {

    private final CustomerRepository repository;

    public CustomerService(CustomerRepository repository) {
        this.repository = repository;
    }

    public String describe(long id) {
        return repository.find(id);
    }
}
