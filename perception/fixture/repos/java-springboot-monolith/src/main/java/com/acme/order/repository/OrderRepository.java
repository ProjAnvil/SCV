package com.acme.order.repository;

import com.acme.order.domain.Order;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import java.time.Instant;
import java.util.List;

/** Spring Data repository: stereotype Repository, no explicit injection. */
@Repository
public interface OrderRepository extends JpaRepository<Order, String> {

    List<Order> findByCustomerId(String customerId);

    @Query("select o from Order o where o.createdAt >= :from and o.status = :status")
    List<Order> findSince(@Param("from") Instant from, @Param("status") String status);
}
