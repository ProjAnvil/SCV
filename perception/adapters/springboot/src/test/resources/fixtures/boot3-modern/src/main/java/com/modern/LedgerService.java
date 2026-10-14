package com.modern;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

@Component
public class LedgerService {

    private OrderMapper orderMapper;

    @Autowired
    public void setOrderMapper(OrderMapper orderMapper) {
        this.orderMapper = orderMapper;
    }
}
